import { describe, expect, it } from 'vitest';
import type { Page } from 'playwright';
import { pino } from 'pino';
import { runJob, type Recorder, type WorkerDeps } from '../src/worker.js';
import type { JobRecord, JobStatus, WorkerEvent } from '../src/scheduler.js';
import type { JoinOptions, MeetingPlatform, PlatformName } from '../src/platforms/types.js';

const job: JobRecord = {
  id: 'job-1',
  platform: 'meet',
  meetingUrl: 'https://meet.google.com/abc-defg-hij',
  status: 'scheduled',
  createdAt: '2026-09-03T10:00:00.000Z',
  startedAt: null,
  endedAt: null,
  onBehalfOf: 'Jude Otine',
};

interface Harness {
  calls: string[];
  events: WorkerEvent[];
  statuses: JobStatus[];
  controller: AbortController;
  deps: WorkerDeps;
  consentText: string | null;
}

interface HarnessHooks {
  onJoin?: (harness: Harness) => void | Promise<void>;
  onConsent?: (harness: Harness) => void | Promise<void>;
  onRecordStart?: (harness: Harness) => void | Promise<void>;
  onPoll?: (harness: Harness) => void | Promise<void>;
  onStop?: (harness: Harness) => void | Promise<void>;
}

function makeHarness(hooks: HarnessHooks = {}): Harness {
  const calls: string[] = [];
  const events: WorkerEvent[] = [];
  const controller = new AbortController();
  let pollTicks = 0;

  const harness = {
    calls,
    events,
    controller,
    consentText: null,
  } as Harness;

  const platform: MeetingPlatform = {
    name: 'meet' as PlatformName,
    async join(_page: Page, _opts: JoinOptions) {
      calls.push('join');
      await hooks.onJoin?.(harness);
    },
    async announceConsent(_page: Page, text: string) {
      calls.push('announceConsent');
      harness.consentText = text;
      await hooks.onConsent?.(harness);
    },
    async participantCount() {
      return 3;
    },
    async isMeetingOver() {
      pollTicks += 1;
      await hooks.onPoll?.(harness);
      return pollTicks >= 2;
    },
    async leave() {
      calls.push('leave');
    },
  };

  let running = false;
  const recorder: Recorder = {
    async setup() {
      calls.push('recorder.setup');
      return 1;
    },
    async start(outFile: string) {
      running = true;
      calls.push(`recorder.start:${outFile.endsWith('meeting.wav')}`);
      await hooks.onRecordStart?.(harness);
    },
    async stop() {
      if (!running) {
        return null;
      }
      running = false;
      calls.push('recorder.stop');
      await hooks.onStop?.(harness);
      return null;
    },
    async teardown() {
      calls.push('recorder.teardown');
    },
  };

  harness.deps = {
    signal: controller.signal,
    logger: pino({ level: 'silent' }),
    platform,
    recorder,
    emit: (event) => {
      events.push(event);
    },
    async openPage() {
      calls.push('openPage');
      return {
        page: {} as Page,
        close: async () => {
          calls.push('session.close');
        },
      };
    },
    async transcribe() {
      calls.push('transcribe');
      return { transcriptPath: '/rec/job-1/transcripts.json' };
    },
    async ensureDir() {
      /* no directories are created in tests */
    },
    pollIntervalMs: 1,
  };

  Object.defineProperty(harness, 'statuses', {
    get: () =>
      events
        .filter((event) => event.type === 'status')
        .map((event) => event.status as JobStatus),
  });

  return harness;
}

describe('runJob', () => {
  it('announces consent before the recorder is ever started', async () => {
    const harness = makeHarness();
    await runJob(job, harness.deps);

    const consentAt = harness.calls.indexOf('announceConsent');
    const recordAt = harness.calls.findIndex((call) => call.startsWith('recorder.start'));
    expect(consentAt).toBeGreaterThanOrEqual(0);
    expect(recordAt).toBeGreaterThan(consentAt);
    expect(harness.consentText).toContain('on behalf of Jude Otine');
  });

  it('runs the full lifecycle and reports the artifacts', async () => {
    const harness = makeHarness();
    await runJob(job, harness.deps);

    expect(harness.calls).toEqual([
      'recorder.setup',
      'openPage',
      'join',
      'announceConsent',
      'recorder.start:true',
      'leave',
      'recorder.stop',
      'transcribe',
      'recorder.teardown',
      'session.close',
    ]);
    expect(harness.statuses).toEqual(['joining', 'recording', 'transcribing', 'done']);

    const done = harness.events.at(-1);
    expect(done?.artifacts?.transcript).toBe('/rec/job-1/transcripts.json');
    expect(done?.artifacts?.wav).toMatch(/job-1\/meeting\.wav$/);
  });

  it('cancelled during the lobby wait: never announces consent, never records', async () => {
    const harness = makeHarness({
      onJoin: (h) => {
        h.controller.abort();
      },
    });

    await runJob(job, harness.deps);

    expect(harness.calls).not.toContain('announceConsent');
    expect(harness.calls.some((call) => call.startsWith('recorder.start'))).toBe(false);
    expect(harness.calls).toContain('leave');
    expect(harness.calls).toContain('recorder.teardown');
    expect(harness.statuses.at(-1)).toBe('cancelled');
  });

  it('cancelled while announcing consent: the recorder never starts', async () => {
    const harness = makeHarness({
      onConsent: (h) => {
        h.controller.abort();
      },
    });

    await runJob(job, harness.deps);

    expect(harness.calls.some((call) => call.startsWith('recorder.start'))).toBe(false);
    expect(harness.calls).not.toContain('transcribe');
    expect(harness.statuses).toEqual(['joining', 'cancelled']);
  });

  it('cancelled while recording: leaves, stops the recorder, does not transcribe', async () => {
    const harness = makeHarness({
      onPoll: (h) => {
        h.controller.abort();
      },
    });

    await runJob(job, harness.deps);

    expect(harness.calls).toContain('leave');
    expect(harness.calls).toContain('recorder.stop');
    expect(harness.calls).not.toContain('transcribe');
    expect(harness.statuses).toEqual(['joining', 'recording', 'cancelled']);
  });

  it('cancelled after the recorder stopped: transcription is skipped', async () => {
    const harness = makeHarness({
      onStop: (h) => {
        h.controller.abort();
      },
    });

    await runJob(job, harness.deps);

    expect(harness.calls).not.toContain('transcribe');
    expect(harness.statuses.at(-1)).toBe('cancelled');
  });

  it('reports a failure with its message and still cleans up', async () => {
    const harness = makeHarness({
      onJoin: () => {
        throw new Error('AdmissionTimeout: nobody let the notetaker in');
      },
    });

    await runJob(job, harness.deps);

    expect(harness.statuses.at(-1)).toBe('failed');
    expect(harness.events.at(-1)?.error).toMatch(/AdmissionTimeout/);
    expect(harness.calls).toContain('recorder.teardown');
    expect(harness.calls).toContain('session.close');
  });
});
