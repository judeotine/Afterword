/**
 * In-memory job store and scheduler.
 *
 * The scheduler never touches Playwright or PulseAudio: it owns job records,
 * timers and the newline-delimited JSON protocol spoken by worker.ts. The
 * actual spawning sits behind the JobRunner interface so the transitions and
 * the parser can be unit-tested without a child process.
 */
import { spawn } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { UnsupportedPlatformError } from './errors.js';
import { detectPlatform, SUPPORTED_PLATFORMS, type PlatformName } from './platforms/types.js';

export type JobStatus =
  | 'scheduled'
  | 'joining'
  | 'recording'
  | 'transcribing'
  | 'done'
  | 'failed'
  | 'cancelled';

export interface JobArtifacts {
  wav: string;
  transcript: string;
}

export interface JobRecord {
  id: string;
  platform: PlatformName;
  meetingUrl: string;
  status: JobStatus;
  createdAt: string;
  startAt?: string;
  botName?: string;
  onBehalfOf?: string;
  consentMessage?: string;
  startedAt: string | null;
  endedAt: string | null;
  error?: string;
  artifacts?: JobArtifacts;
}

export interface CreateJobInput {
  meetingUrl: string;
  botName?: string | undefined;
  startAt?: string | undefined;
  onBehalfOf?: string | undefined;
  consentMessage?: string | undefined;
}

/** One line of the worker's stdout protocol. */
export interface WorkerEvent {
  type: 'status' | 'log';
  status?: JobStatus;
  error?: string;
  artifacts?: JobArtifacts;
  message?: string;
  level?: string;
  at?: string;
}

export interface JobRunHandle {
  /** Ask the worker to leave the meeting and shut down. */
  cancel(): void;
}

export interface JobRunHooks {
  onEvent(event: WorkerEvent): void;
  onExit(code: number | null): void;
}

export interface JobRunner {
  start(job: JobRecord, hooks: JobRunHooks): JobRunHandle;
}

/** setTimeout stores its delay in a 32-bit signed int; anything larger fires at once. */
const MAX_TIMEOUT_MS = 2_147_483_647;

const LIVE_STATUSES: readonly JobStatus[] = ['scheduled', 'joining', 'recording', 'transcribing'];

export const TERMINAL_STATUSES: readonly JobStatus[] = ['done', 'failed', 'cancelled'];

/** Legal status moves. Anything else is dropped, so a noisy worker cannot corrupt a record. */
export const ALLOWED_TRANSITIONS: Record<JobStatus, readonly JobStatus[]> = {
  scheduled: ['joining', 'failed', 'cancelled'],
  joining: ['recording', 'failed', 'cancelled'],
  recording: ['transcribing', 'failed', 'cancelled'],
  transcribing: ['done', 'failed', 'cancelled'],
  done: [],
  failed: [],
  cancelled: [],
};

export function canTransition(from: JobStatus, to: JobStatus): boolean {
  return ALLOWED_TRANSITIONS[from].includes(to);
}

export function isTerminal(status: JobStatus): boolean {
  return TERMINAL_STATUSES.includes(status);
}

/** Split a stdout buffer into events, returning the unterminated tail. */
export function parseEventLines(buffer: string): { events: WorkerEvent[]; rest: string } {
  const parts = buffer.split('\n');
  const rest = parts.pop() ?? '';
  const events: WorkerEvent[] = [];

  for (const line of parts) {
    const trimmed = line.trim();
    if (!trimmed) {
      continue;
    }
    try {
      const parsed: unknown = JSON.parse(trimmed);
      if (parsed && typeof parsed === 'object' && 'type' in parsed) {
        events.push(parsed as WorkerEvent);
      }
    } catch {
      // Not our protocol (library logging, stack traces): ignore.
    }
  }

  return { events, rest };
}

/** Fold one worker event into a job record. Pure: returns a new record. */
export function applyEvent(job: JobRecord, event: WorkerEvent, now: string): JobRecord {
  if (event.type !== 'status' || !event.status) {
    return job;
  }
  if (!canTransition(job.status, event.status)) {
    return job;
  }

  const next: JobRecord = { ...job, status: event.status };

  if (LIVE_STATUSES.includes(event.status) && next.startedAt === null) {
    next.startedAt = event.at ?? now;
  }
  if (isTerminal(event.status)) {
    next.endedAt = event.at ?? now;
  }
  if (event.error) {
    next.error = event.error;
  }
  if (event.artifacts) {
    next.artifacts = event.artifacts;
  }

  return next;
}

export interface SchedulerOptions {
  runner: JobRunner;
  now?: () => Date;
  idFactory?: () => string;
}

interface JobSlot {
  record: JobRecord;
  timer?: ReturnType<typeof setTimeout>;
  handle?: JobRunHandle;
}

export class Scheduler {
  private readonly runner: JobRunner;
  private readonly now: () => Date;
  private readonly idFactory: () => string;
  private readonly jobs = new Map<string, JobSlot>();

  constructor({ runner, now = () => new Date(), idFactory = () => randomUUID() }: SchedulerOptions) {
    this.runner = runner;
    this.now = now;
    this.idFactory = idFactory;
  }

  /** Validate the URL, record the job, and run it now or at startAt. */
  create(input: CreateJobInput): JobRecord {
    const platform = detectPlatform(input.meetingUrl);
    if (!platform) {
      throw new UnsupportedPlatformError(input.meetingUrl, SUPPORTED_PLATFORMS);
    }

    const record: JobRecord = {
      id: this.idFactory(),
      platform: platform.name,
      meetingUrl: input.meetingUrl,
      status: 'scheduled',
      createdAt: this.now().toISOString(),
      startedAt: null,
      endedAt: null,
    };
    if (input.startAt) {
      record.startAt = input.startAt;
    }
    if (input.botName) {
      record.botName = input.botName;
    }
    if (input.onBehalfOf) {
      record.onBehalfOf = input.onBehalfOf;
    }
    if (input.consentMessage) {
      record.consentMessage = input.consentMessage;
    }

    const slot: JobSlot = { record };
    this.jobs.set(record.id, slot);

    const startAtMs = input.startAt ? new Date(input.startAt).getTime() : Number.NaN;
    if (Number.isFinite(startAtMs) && startAtMs > this.now().getTime()) {
      this.arm(slot, startAtMs);
    } else {
      this.run(record.id);
    }

    return { ...record };
  }

  get(id: string): JobRecord | undefined {
    const slot = this.jobs.get(id);
    return slot ? { ...slot.record } : undefined;
  }

  list(): JobRecord[] {
    return [...this.jobs.values()].map((slot) => ({ ...slot.record }));
  }

  /** Cancel a pending or live job. Returns false when it is unknown or finished. */
  cancel(id: string): boolean {
    const slot = this.jobs.get(id);
    if (!slot || isTerminal(slot.record.status)) {
      return false;
    }

    if (slot.timer) {
      clearTimeout(slot.timer);
      delete slot.timer;
    }

    if (slot.handle) {
      slot.handle.cancel();
    } else {
      slot.record = applyEvent(
        slot.record,
        { type: 'status', status: 'cancelled' },
        this.now().toISOString(),
      );
    }
    return true;
  }

  /**
   * Sleep until `startAtMs`, in hops no longer than setTimeout can represent —
   * a longer delay overflows the 32-bit timer and fires immediately.
   */
  private arm(slot: JobSlot, startAtMs: number): void {
    const remaining = startAtMs - this.now().getTime();
    if (remaining <= 0) {
      delete slot.timer;
      this.run(slot.record.id);
      return;
    }

    slot.timer = setTimeout(() => {
      delete slot.timer;
      this.arm(slot, startAtMs);
    }, Math.min(remaining, MAX_TIMEOUT_MS));
    if (typeof slot.timer.unref === 'function') {
      slot.timer.unref();
    }
  }

  private run(id: string): void {
    const slot = this.jobs.get(id);
    if (!slot || isTerminal(slot.record.status)) {
      return;
    }

    slot.handle = this.runner.start({ ...slot.record }, {
      onEvent: (event) => {
        slot.record = applyEvent(slot.record, event, this.now().toISOString());
      },
      onExit: (code) => {
        delete slot.handle;
        if (isTerminal(slot.record.status)) {
          return;
        }
        slot.record = applyEvent(
          slot.record,
          {
            type: 'status',
            status: 'failed',
            error: `Worker exited with code ${code ?? 'null'} before reporting a result`,
          },
          this.now().toISOString(),
        );
      },
    });
  }
}

const here = path.dirname(fileURLToPath(import.meta.url));
const isCompiled = here.split(path.sep).includes('dist');

/** Command used to run one job: compiled worker in production, tsx in dev. */
export function buildWorkerCommand(job: JobRecord, options?: { compiled?: boolean; dir?: string }): {
  command: string;
  args: string[];
} {
  const compiled = options?.compiled ?? isCompiled;
  const dir = options?.dir ?? here;
  const payload = JSON.stringify(job);

  return compiled
    ? { command: process.execPath, args: [path.join(dir, 'worker.js'), payload] }
    : { command: 'tsx', args: [path.join(dir, 'worker.ts'), payload] };
}

/** How long a cancelled worker gets to leave the meeting before it is killed. */
export const DEFAULT_KILL_GRACE_MS = 30_000;

/** The slice of ChildProcess escalatingKill needs, so it can be unit-tested. */
export interface KillableChild {
  kill(signal: NodeJS.Signals): boolean;
  once(event: 'exit', listener: () => void): unknown;
}

/**
 * Ask a worker to shut down, then insist.
 *
 * SIGTERM lets the worker leave the meeting and flush the wav; if it is stuck
 * (a hung Chromium, a wedged parecord) it would otherwise sit in the call
 * forever, so escalate to SIGKILL once the grace period is up.
 */
export function escalatingKill(child: KillableChild, graceMs = DEFAULT_KILL_GRACE_MS): void {
  child.kill('SIGTERM');

  const timer = setTimeout(() => {
    child.kill('SIGKILL');
  }, graceMs);
  if (typeof timer.unref === 'function') {
    timer.unref();
  }

  child.once('exit', () => {
    clearTimeout(timer);
  });
}

export interface ChildProcessRunnerOptions {
  /** Grace period between SIGTERM and SIGKILL on cancel. */
  killGraceMs?: number;
}

/** Runs each job as a child process and forwards its stdout protocol. */
export class ChildProcessRunner implements JobRunner {
  private readonly killGraceMs: number;

  constructor({ killGraceMs = DEFAULT_KILL_GRACE_MS }: ChildProcessRunnerOptions = {}) {
    this.killGraceMs = killGraceMs;
  }

  start(job: JobRecord, hooks: JobRunHooks): JobRunHandle {
    const { command, args } = buildWorkerCommand(job);
    const child = spawn(command, args, {
      stdio: ['ignore', 'pipe', 'inherit'],
      env: process.env,
    });

    let buffer = '';
    child.stdout.setEncoding('utf8');
    child.stdout.on('data', (chunk: string) => {
      buffer += chunk;
      const { events, rest } = parseEventLines(buffer);
      buffer = rest;
      for (const event of events) {
        hooks.onEvent(event);
      }
    });

    child.on('error', (error: Error) => {
      hooks.onEvent({ type: 'status', status: 'failed', error: error.message });
    });
    child.on('exit', (code) => {
      hooks.onExit(code);
    });

    return {
      cancel: () => {
        escalatingKill(child, this.killGraceMs);
      },
    };
  }
}
