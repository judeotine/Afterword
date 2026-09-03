/**
 * One meeting, one process.
 *
 * The worker joins the meeting, announces consent, records the call audio from
 * a PulseAudio null sink and transcribes it with the Rust CLI. Progress is
 * reported to the scheduler as newline-delimited JSON on stdout, so *nothing*
 * else may be written there — pino logs go to stderr.
 *
 * Two rules are enforced by the ordering below and covered by test/worker.test.ts:
 *   1. announceConsent() runs before recorder.start(), always.
 *   2. once the job is cancelled, no further stage is entered — the bot leaves,
 *      the recorder stops, and transcription is skipped.
 *
 * Everything external (browser, recorder, transcriber) arrives through
 * WorkerDeps so the lifecycle can be tested without Chromium or PulseAudio.
 */
import { mkdir } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { pino, type Logger } from 'pino';
import { chromium, type Page } from 'playwright';
import { PulseAudio } from './audio/pulse.js';
import { config, type BotConfig } from './config.js';
import { buildConsentMessage } from './consent.js';
import { UnsupportedPlatformError } from './errors.js';
import { detectPlatform, SUPPORTED_PLATFORMS, type MeetingPlatform } from './platforms/types.js';
import { runTranscribe } from './transcribe.js';
import type { JobArtifacts, JobRecord, JobStatus, WorkerEvent } from './scheduler.js';

const defaultLogger: Logger = pino({ name: 'afterword-bot-worker' }, pino.destination(2));

const DEFAULT_POLL_INTERVAL_MS = 5_000;

/** Chromium flags that make a headless browser usable as a meeting participant. */
export const CHROMIUM_ARGS = [
  '--use-fake-ui-for-media-stream',
  '--use-fake-device-for-media-stream',
  '--autoplay-policy=no-user-gesture-required',
  '--disable-blink-features=AutomationControlled',
];

/** The recorder contract runJob depends on; PulseAudio implements it. */
export interface Recorder {
  setup(): Promise<unknown>;
  start(outFile: string): Promise<void>;
  stop(): Promise<unknown>;
  teardown(): Promise<void>;
}

export interface PageSession {
  page: Page;
  close(): Promise<void>;
}

export interface WorkerDeps {
  /** Writes one protocol line. */
  emit(event: WorkerEvent): void;
  /** Aborted when the job is cancelled (SIGTERM). */
  signal: AbortSignal;
  openPage(): Promise<PageSession>;
  recorder: Recorder;
  transcribe(input: {
    wav: string;
    outDir: string;
    signal: AbortSignal;
  }): Promise<{ transcriptPath: string }>;
  ensureDir(dir: string): Promise<void>;
  /** Defaults to detectPlatform(job.meetingUrl). */
  platform?: MeetingPlatform | null;
  settings?: BotConfig;
  pollIntervalMs?: number;
  /** Defaults to the stderr pino logger; tests silence it. */
  logger?: Logger;
}

/** Thrown at a stage boundary once the job has been cancelled. */
class CancelledError extends Error {
  constructor(stage: string) {
    super(`Cancelled before ${stage}`);
    this.name = 'CancelledError';
  }
}

function emitTo(deps: WorkerDeps, event: WorkerEvent): void {
  deps.emit(event);
}

function emitStatus(deps: WorkerDeps, status: JobStatus, extra: Partial<WorkerEvent> = {}): void {
  emitTo(deps, { type: 'status', status, ...extra });
}

/** Sleep that wakes immediately when the job is cancelled. */
function sleep(ms: number, signal: AbortSignal): Promise<void> {
  if (signal.aborted) {
    return Promise.resolve();
  }
  return new Promise((resolve) => {
    const timer = setTimeout(finish, ms);
    function finish(): void {
      clearTimeout(timer);
      signal.removeEventListener('abort', finish);
      resolve();
    }
    signal.addEventListener('abort', finish, { once: true });
  });
}

async function pollUntilOver(
  platform: MeetingPlatform,
  page: Page,
  deps: WorkerDeps,
  settings: BotConfig,
): Promise<string> {
  const startedAt = Date.now();
  const maxMs = settings.MAX_MEETING_MINUTES * 60_000;
  const aloneMs = settings.ALONE_TIMEOUT_SECONDS * 1_000;
  const interval = deps.pollIntervalMs ?? DEFAULT_POLL_INTERVAL_MS;
  let aloneSince: number | null = null;

  for (;;) {
    if (deps.signal.aborted) {
      return 'cancelled';
    }
    if (Date.now() - startedAt > maxMs) {
      return `max meeting length of ${settings.MAX_MEETING_MINUTES} minutes reached`;
    }
    if (await platform.isMeetingOver(page).catch(() => false)) {
      return 'the meeting ended';
    }
    if (deps.signal.aborted) {
      return 'cancelled';
    }

    const participants = await platform.participantCount(page).catch(() => null);
    if (participants !== null && participants <= 1) {
      aloneSince ??= Date.now();
      if (Date.now() - aloneSince > aloneMs) {
        return `alone in the meeting for ${settings.ALONE_TIMEOUT_SECONDS}s`;
      }
    } else {
      aloneSince = null;
    }

    await sleep(interval, deps.signal);
  }
}

/**
 * Run one meeting to completion. Never throws for an expected outcome: the
 * result is reported through status events (done / cancelled / failed).
 */
export async function runJob(job: JobRecord, deps: WorkerDeps): Promise<void> {
  const settings = deps.settings ?? config;
  const log = deps.logger ?? defaultLogger;
  const platform = deps.platform ?? detectPlatform(job.meetingUrl);
  if (!platform) {
    emitStatus(deps, 'failed', {
      error: new UnsupportedPlatformError(job.meetingUrl, SUPPORTED_PLATFORMS).message,
    });
    return;
  }

  const botName = job.botName ?? settings.BOT_NAME;
  const consentMessage =
    job.consentMessage ??
    buildConsentMessage({
      botName,
      onBehalfOf: job.onBehalfOf,
      privacyUrl: settings.PRIVACY_URL,
    });

  const outDir = path.resolve(settings.RECORDINGS_DIR, job.id);
  const wavPath = path.join(outDir, 'meeting.wav');

  const checkpoint = (stage: string): void => {
    if (deps.signal.aborted) {
      throw new CancelledError(stage);
    }
  };

  let session: PageSession | null = null;
  let joined = false;
  let left = false;
  const leaveOnce = async (): Promise<void> => {
    if (!joined || left || !session) {
      return;
    }
    left = true;
    await platform.leave(session.page).catch((error: unknown) => {
      log.warn({ err: String(error) }, 'leaving the meeting failed');
    });
  };

  try {
    await deps.ensureDir(outDir);
    await deps.recorder.setup();

    session = await deps.openPage();

    emitStatus(deps, 'joining');
    await platform.join(session.page, {
      botName,
      meetingUrl: job.meetingUrl,
      signal: deps.signal,
    });
    joined = true;

    checkpoint('announcing consent');
    await platform.announceConsent(session.page, consentMessage);

    // Consent before capture: this checkpoint and the two lines around it are
    // the product rule. Do not start the recorder above them.
    checkpoint('starting the recorder');
    await deps.recorder.start(wavPath);
    emitStatus(deps, 'recording');

    const reason = await pollUntilOver(platform, session.page, deps, settings);
    log.info({ reason }, 'leaving the meeting');

    await leaveOnce();
    await deps.recorder.stop();

    checkpoint('transcribing');
    emitStatus(deps, 'transcribing');
    const { transcriptPath } = await deps.transcribe({
      wav: wavPath,
      outDir,
      signal: deps.signal,
    });

    const artifacts: JobArtifacts = { wav: wavPath, transcript: transcriptPath };
    emitStatus(deps, 'done', { artifacts });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    if (deps.signal.aborted) {
      log.info({ reason: message }, 'job cancelled');
      emitStatus(deps, 'cancelled');
    } else {
      log.error({ err: message }, 'job failed');
      emitStatus(deps, 'failed', { error: message });
    }
  } finally {
    await leaveOnce();
    await deps.recorder.stop().catch(() => undefined);
    await deps.recorder.teardown().catch(() => undefined);
    if (session) {
      await session.close().catch(() => undefined);
    }
  }
}

/** Real dependencies: Chromium, the PulseAudio sink and the Rust CLI. */
export function defaultDeps(signal: AbortSignal, settings: BotConfig = config): WorkerDeps {
  const pulse = new PulseAudio(settings.PULSE_SINK_NAME);

  return {
    signal,
    settings,
    recorder: pulse,
    emit: (event) => {
      process.stdout.write(`${JSON.stringify({ ...event, at: new Date().toISOString() })}\n`);
    },
    ensureDir: async (dir) => {
      await mkdir(dir, { recursive: true });
    },
    openPage: async () => {
      const browser = await chromium.launch({
        headless: settings.HEADLESS,
        args: CHROMIUM_ARGS,
        env: { ...process.env, PULSE_SINK: settings.PULSE_SINK_NAME },
      });
      const context = await browser.newContext();
      const page = await context.newPage();
      return {
        page,
        close: async () => {
          await browser.close();
        },
      };
    },
    transcribe: ({ wav, outDir, signal: cancelSignal }) =>
      runTranscribe({ wav, outDir, signal: cancelSignal }, settings),
  };
}

export function parseJobArgv(argv: readonly string[]): JobRecord {
  const raw = argv[2];
  if (!raw) {
    throw new Error('worker requires the job record as JSON on argv[2]');
  }
  return JSON.parse(raw) as JobRecord;
}

async function main(): Promise<void> {
  const controller = new AbortController();
  const onSignal = (): void => {
    defaultLogger.info('termination signal received; leaving the meeting');
    controller.abort();
  };
  process.on('SIGTERM', onSignal);
  process.on('SIGINT', onSignal);

  let job: JobRecord;
  try {
    job = parseJobArgv(process.argv);
  } catch (error) {
    process.stdout.write(
      `${JSON.stringify({
        type: 'status',
        status: 'failed',
        error: error instanceof Error ? error.message : String(error),
        at: new Date().toISOString(),
      })}\n`,
    );
    process.exitCode = 1;
    return;
  }

  await runJob(job, defaultDeps(controller.signal));

  process.off('SIGTERM', onSignal);
  process.off('SIGINT', onSignal);
}

const invokedDirectly =
  process.argv[1] !== undefined && fileURLToPath(import.meta.url) === process.argv[1];

if (invokedDirectly) {
  await main();
}
