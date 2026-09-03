/**
 * One meeting, one process.
 *
 * The worker joins the meeting, announces consent, records the call audio from
 * a PulseAudio null sink and transcribes it with the Rust CLI. Progress is
 * reported to the scheduler as newline-delimited JSON on stdout, so *nothing*
 * else may be written there — pino logs go to stderr.
 *
 * Consent rule: announceConsent() runs before recorder.start(), always.
 */
import { mkdir } from 'node:fs/promises';
import path from 'node:path';
import { pino } from 'pino';
import { chromium, type Browser, type Page } from 'playwright';
import { PulseAudio } from './audio/pulse.js';
import { config } from './config.js';
import { buildConsentMessage } from './consent.js';
import { UnsupportedPlatformError } from './errors.js';
import { detectPlatform, SUPPORTED_PLATFORMS, type MeetingPlatform } from './platforms/types.js';
import { runTranscribe } from './transcribe.js';
import type { JobArtifacts, JobRecord, JobStatus, WorkerEvent } from './scheduler.js';

const log = pino({ name: 'afterword-bot-worker' }, pino.destination(2));

const POLL_INTERVAL_MS = 5_000;

/** Chromium flags that make a headless browser usable as a meeting participant. */
export const CHROMIUM_ARGS = [
  '--use-fake-ui-for-media-stream',
  '--use-fake-device-for-media-stream',
  '--autoplay-policy=no-user-gesture-required',
  '--disable-blink-features=AutomationControlled',
];

function emit(event: WorkerEvent): void {
  process.stdout.write(`${JSON.stringify({ ...event, at: new Date().toISOString() })}\n`);
}

function emitStatus(status: JobStatus, extra: Partial<WorkerEvent> = {}): void {
  emit({ type: 'status', status, ...extra });
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

interface StopSignal {
  cancelled: boolean;
}

async function pollUntilOver(
  platform: MeetingPlatform,
  page: Page,
  stop: StopSignal,
): Promise<string> {
  const startedAt = Date.now();
  const maxMs = config.MAX_MEETING_MINUTES * 60_000;
  const aloneMs = config.ALONE_TIMEOUT_SECONDS * 1_000;
  let aloneSince: number | null = null;

  for (;;) {
    if (stop.cancelled) {
      return 'cancelled';
    }
    if (Date.now() - startedAt > maxMs) {
      return `max meeting length of ${config.MAX_MEETING_MINUTES} minutes reached`;
    }
    if (await platform.isMeetingOver(page).catch(() => false)) {
      return 'the meeting ended';
    }

    const participants = await platform.participantCount(page).catch(() => null);
    if (participants !== null && participants <= 1) {
      aloneSince ??= Date.now();
      if (Date.now() - aloneSince > aloneMs) {
        return `alone in the meeting for ${config.ALONE_TIMEOUT_SECONDS}s`;
      }
    } else {
      aloneSince = null;
    }

    await sleep(POLL_INTERVAL_MS);
  }
}

export async function runJob(job: JobRecord): Promise<void> {
  const platform = detectPlatform(job.meetingUrl);
  if (!platform) {
    throw new UnsupportedPlatformError(job.meetingUrl, SUPPORTED_PLATFORMS);
  }

  const botName = job.botName ?? config.BOT_NAME;
  const consentMessage =
    job.consentMessage ??
    buildConsentMessage({
      botName,
      onBehalfOf: job.onBehalfOf,
      privacyUrl: config.PRIVACY_URL,
    });

  const outDir = path.resolve(config.RECORDINGS_DIR, job.id);
  const wavPath = path.join(outDir, 'meeting.wav');
  await mkdir(outDir, { recursive: true });

  const pulse = new PulseAudio(config.PULSE_SINK_NAME);
  const stop: StopSignal = { cancelled: false };
  let browser: Browser | null = null;
  let page: Page | null = null;
  let recorderStarted = false;

  const onSigterm = (): void => {
    stop.cancelled = true;
    log.info('SIGTERM received; leaving the meeting');
  };
  process.on('SIGTERM', onSigterm);
  process.on('SIGINT', onSigterm);

  try {
    await pulse.setup();

    browser = await chromium.launch({
      headless: config.HEADLESS,
      args: CHROMIUM_ARGS,
      env: { ...process.env, PULSE_SINK: config.PULSE_SINK_NAME },
    });
    const context = await browser.newContext();
    page = await context.newPage();

    emitStatus('joining');
    await platform.join(page, { botName, meetingUrl: job.meetingUrl });

    // Consent before capture: never move these two lines apart.
    await platform.announceConsent(page, consentMessage);
    await pulse.start(wavPath);
    recorderStarted = true;
    emitStatus('recording');

    const reason = await pollUntilOver(platform, page, stop);
    log.info({ reason }, 'leaving the meeting');

    await platform.leave(page).catch(() => undefined);
    await pulse.stop();
    recorderStarted = false;

    if (stop.cancelled) {
      emitStatus('cancelled');
      return;
    }

    emitStatus('transcribing');
    const { transcriptPath } = await runTranscribe({ wav: wavPath, outDir });

    const artifacts: JobArtifacts = { wav: wavPath, transcript: transcriptPath };
    emitStatus('done', { artifacts });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    log.error({ err: message }, 'job failed');
    if (recorderStarted) {
      await pulse.stop().catch(() => undefined);
    }
    emitStatus(stop.cancelled ? 'cancelled' : 'failed', stop.cancelled ? {} : { error: message });
    throw error;
  } finally {
    process.off('SIGTERM', onSigterm);
    process.off('SIGINT', onSigterm);
    await pulse.teardown().catch(() => undefined);
    if (browser) {
      await browser.close().catch(() => undefined);
    }
  }
}

function parseJobArgv(argv: readonly string[]): JobRecord {
  const raw = argv[2];
  if (!raw) {
    throw new Error('worker requires the job record as JSON on argv[2]');
  }
  return JSON.parse(raw) as JobRecord;
}

async function main(): Promise<void> {
  let job: JobRecord;
  try {
    job = parseJobArgv(process.argv);
  } catch (error) {
    emitStatus('failed', { error: error instanceof Error ? error.message : String(error) });
    process.exitCode = 1;
    return;
  }

  try {
    await runJob(job);
  } catch {
    // runJob already emitted the failure event.
    process.exitCode = 1;
  }
}

await main();
