import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  DEFAULT_KILL_GRACE_MS,
  Scheduler,
  applyEvent,
  canTransition,
  escalatingKill,
  parseEventLines,
  type JobRecord,
  type JobRunHandle,
  type JobRunner,
  type WorkerEvent,
} from '../src/scheduler.js';

class FakeRunner implements JobRunner {
  started: JobRecord[] = [];
  cancelled: string[] = [];
  private hooks = new Map<
    string,
    { onEvent: (event: WorkerEvent) => void; onExit: (code: number | null) => void }
  >();

  start(
    job: JobRecord,
    hooks: { onEvent: (event: WorkerEvent) => void; onExit: (code: number | null) => void },
  ): JobRunHandle {
    this.started.push(job);
    this.hooks.set(job.id, hooks);
    return {
      cancel: () => {
        this.cancelled.push(job.id);
      },
    };
  }

  emit(id: string, event: WorkerEvent): void {
    this.hooks.get(id)?.onEvent(event);
  }

  exit(id: string, code: number | null): void {
    this.hooks.get(id)?.onExit(code);
  }
}

const MEET_URL = 'https://meet.google.com/abc-defg-hij';

describe('parseEventLines', () => {
  it('parses newline-delimited JSON events and keeps the partial tail', () => {
    const { events, rest } = parseEventLines(
      '{"type":"status","status":"joining"}\n{"type":"status","status":"reco',
    );
    expect(events).toEqual([{ type: 'status', status: 'joining' }]);
    expect(rest).toBe('{"type":"status","status":"reco');
  });

  it('ignores blank lines and non-JSON noise from the child stdout', () => {
    const { events, rest } = parseEventLines('\nnot json\n{"type":"log","message":"hi"}\n');
    expect(events).toEqual([{ type: 'log', message: 'hi' }]);
    expect(rest).toBe('');
  });
});

describe('escalatingKill', () => {
  interface FakeChild {
    kill(signal: NodeJS.Signals): boolean;
    once(event: 'exit', listener: () => void): unknown;
  }

  function fakeChild(): { child: FakeChild; signals: string[]; exit: () => void } {
    const signals: string[] = [];
    let onExit: (() => void) | null = null;
    return {
      signals,
      exit: () => onExit?.(),
      child: {
        kill(signal) {
          signals.push(signal);
          return true;
        },
        once(_event, listener) {
          onExit = listener;
          return this;
        },
      },
    };
  }

  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('asks the worker to leave with SIGTERM first', () => {
    const { child, signals } = fakeChild();
    escalatingKill(child, DEFAULT_KILL_GRACE_MS);
    expect(signals).toEqual(['SIGTERM']);
  });

  it('escalates to SIGKILL when the worker ignores SIGTERM', () => {
    const { child, signals } = fakeChild();
    escalatingKill(child, 30_000);

    vi.advanceTimersByTime(29_999);
    expect(signals).toEqual(['SIGTERM']);

    vi.advanceTimersByTime(2);
    expect(signals).toEqual(['SIGTERM', 'SIGKILL']);
  });

  it('does not SIGKILL a worker that left gracefully in time', () => {
    const { child, signals, exit } = fakeChild();
    escalatingKill(child, 30_000);

    exit();
    vi.advanceTimersByTime(60_000);
    expect(signals).toEqual(['SIGTERM']);
  });
});

describe('canTransition', () => {
  it('allows the forward lifecycle', () => {
    expect(canTransition('scheduled', 'joining')).toBe(true);
    expect(canTransition('joining', 'recording')).toBe(true);
    expect(canTransition('recording', 'transcribing')).toBe(true);
    expect(canTransition('transcribing', 'done')).toBe(true);
  });

  it('allows failure and cancellation from any live state', () => {
    for (const from of ['scheduled', 'joining', 'recording', 'transcribing'] as const) {
      expect(canTransition(from, 'failed')).toBe(true);
      expect(canTransition(from, 'cancelled')).toBe(true);
    }
  });

  it('refuses to move backwards or out of a terminal state', () => {
    expect(canTransition('recording', 'joining')).toBe(false);
    expect(canTransition('done', 'recording')).toBe(false);
    expect(canTransition('cancelled', 'done')).toBe(false);
    expect(canTransition('failed', 'done')).toBe(false);
  });
});

describe('applyEvent', () => {
  const base: JobRecord = {
    id: 'job-1',
    platform: 'meet',
    meetingUrl: MEET_URL,
    status: 'scheduled',
    createdAt: '2026-09-03T10:00:00.000Z',
    startedAt: null,
    endedAt: null,
  };

  it('stamps startedAt on the first live status', () => {
    const next = applyEvent(base, { type: 'status', status: 'joining' }, '2026-09-03T10:01:00.000Z');
    expect(next.status).toBe('joining');
    expect(next.startedAt).toBe('2026-09-03T10:01:00.000Z');
    expect(next.endedAt).toBeNull();
  });

  it('stamps endedAt and artifacts when the worker reports done', () => {
    const next = applyEvent(
      { ...base, status: 'transcribing', startedAt: '2026-09-03T10:01:00.000Z' },
      {
        type: 'status',
        status: 'done',
        artifacts: { wav: '/rec/job-1.wav', transcript: '/rec/job-1/transcripts.json' },
      },
      '2026-09-03T10:30:00.000Z',
    );
    expect(next.status).toBe('done');
    expect(next.endedAt).toBe('2026-09-03T10:30:00.000Z');
    expect(next.artifacts).toEqual({
      wav: '/rec/job-1.wav',
      transcript: '/rec/job-1/transcripts.json',
    });
  });

  it('records the error message on failure', () => {
    const next = applyEvent(
      { ...base, status: 'joining' },
      { type: 'status', status: 'failed', error: 'AdmissionTimeout' },
      '2026-09-03T10:12:00.000Z',
    );
    expect(next.status).toBe('failed');
    expect(next.error).toBe('AdmissionTimeout');
    expect(next.endedAt).toBe('2026-09-03T10:12:00.000Z');
  });

  it('ignores log events and illegal transitions', () => {
    const done: JobRecord = { ...base, status: 'done', endedAt: '2026-09-03T10:30:00.000Z' };
    expect(applyEvent(done, { type: 'log', message: 'noise' }, 'now')).toEqual(done);
    expect(applyEvent(done, { type: 'status', status: 'recording' }, 'now')).toEqual(done);
  });
});

describe('Scheduler', () => {
  let runner: FakeRunner;
  let scheduler: Scheduler;

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-09-03T10:00:00.000Z'));
    runner = new FakeRunner();
    let n = 0;
    scheduler = new Scheduler({ runner, idFactory: () => `job-${++n}` });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('runs a job immediately when no startAt is given', () => {
    const job = scheduler.create({ meetingUrl: MEET_URL });
    expect(job.platform).toBe('meet');
    expect(runner.started.map((j) => j.id)).toEqual([job.id]);
  });

  it('defers a job with a future startAt until its time', () => {
    const job = scheduler.create({
      meetingUrl: MEET_URL,
      startAt: '2026-09-03T10:05:00.000Z',
    });
    expect(runner.started).toHaveLength(0);
    expect(scheduler.get(job.id)?.status).toBe('scheduled');

    vi.advanceTimersByTime(5 * 60_000);
    expect(runner.started.map((j) => j.id)).toEqual([job.id]);
  });

  it('does not fire early for a startAt beyond the 32-bit setTimeout limit', () => {
    // setTimeout overflows past ~24.8 days and fires immediately.
    scheduler.create({ meetingUrl: MEET_URL, startAt: '2029-09-03T10:00:00.000Z' });
    vi.advanceTimersByTime(24 * 24 * 60 * 60_000);
    expect(runner.started).toHaveLength(0);

    vi.setSystemTime(new Date('2029-09-03T10:00:00.000Z'));
    vi.advanceTimersByTime(3 * 365 * 24 * 60 * 60_000);
    expect(runner.started).toHaveLength(1);
  });

  it('runs a job whose startAt is already in the past', () => {
    scheduler.create({ meetingUrl: MEET_URL, startAt: '2026-09-03T09:00:00.000Z' });
    expect(runner.started).toHaveLength(1);
  });

  it('updates the job record from worker status events', () => {
    const job = scheduler.create({ meetingUrl: MEET_URL });
    runner.emit(job.id, { type: 'status', status: 'joining' });
    expect(scheduler.get(job.id)?.status).toBe('joining');

    runner.emit(job.id, { type: 'status', status: 'recording' });
    runner.emit(job.id, { type: 'status', status: 'transcribing' });
    runner.emit(job.id, {
      type: 'status',
      status: 'done',
      artifacts: { wav: '/rec/a.wav', transcript: '/rec/a/transcripts.json' },
    });
    runner.exit(job.id, 0);

    const record = scheduler.get(job.id);
    expect(record?.status).toBe('done');
    expect(record?.artifacts?.transcript).toBe('/rec/a/transcripts.json');
  });

  it('fails a job when the worker exits non-zero without a terminal status', () => {
    const job = scheduler.create({ meetingUrl: MEET_URL });
    runner.emit(job.id, { type: 'status', status: 'joining' });
    runner.exit(job.id, 1);
    const record = scheduler.get(job.id);
    expect(record?.status).toBe('failed');
    expect(record?.error).toMatch(/exit/i);
  });

  it('cancels a live job through the runner handle', () => {
    const job = scheduler.create({ meetingUrl: MEET_URL });
    runner.emit(job.id, { type: 'status', status: 'recording' });

    expect(scheduler.cancel(job.id)).toBe(true);
    expect(runner.cancelled).toEqual([job.id]);

    runner.emit(job.id, { type: 'status', status: 'cancelled' });
    expect(scheduler.get(job.id)?.status).toBe('cancelled');
  });

  it('cancels a scheduled job before it ever starts', () => {
    const job = scheduler.create({
      meetingUrl: MEET_URL,
      startAt: '2026-09-03T10:05:00.000Z',
    });
    expect(scheduler.cancel(job.id)).toBe(true);
    vi.advanceTimersByTime(10 * 60_000);
    expect(runner.started).toHaveLength(0);
    expect(scheduler.get(job.id)?.status).toBe('cancelled');
  });

  it('will not cancel an unknown or already finished job', () => {
    expect(scheduler.cancel('nope')).toBe(false);
    const job = scheduler.create({ meetingUrl: MEET_URL });
    runner.emit(job.id, { type: 'status', status: 'joining' });
    runner.emit(job.id, { type: 'status', status: 'failed', error: 'boom' });
    expect(scheduler.cancel(job.id)).toBe(false);
  });

  it('rejects a URL that is not a known meeting platform', () => {
    expect(() => scheduler.create({ meetingUrl: 'https://example.com/x' })).toThrow();
  });
});
