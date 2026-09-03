/**
 * PulseAudio plumbing.
 *
 * The browser plays the meeting into a null sink; `parecord` records that
 * sink's monitor into a 16 kHz mono wav, which is exactly what the Rust
 * transcription CLI expects.
 */
import { execa, type ResultPromise } from 'execa';

export interface ParecordOptions {
  sinkName: string;
  outFile: string;
}

/** `pactl load-module module-null-sink sink_name=<name>` */
export function buildLoadModuleArgs(sinkName: string): string[] {
  return ['load-module', 'module-null-sink', `sink_name=${sinkName}`];
}

/** `pactl unload-module <id>` */
export function buildUnloadModuleArgs(moduleId: number): string[] {
  return ['unload-module', String(moduleId)];
}

/** `parecord --device=<sink>.monitor --file-format=wav --channels=1 --rate=16000 --format=s16le <out>` */
export function buildParecordArgs({ sinkName, outFile }: ParecordOptions): string[] {
  return [
    `--device=${sinkName}.monitor`,
    '--file-format=wav',
    '--channels=1',
    '--rate=16000',
    '--format=s16le',
    outFile,
  ];
}

/** pactl prints the new module id (and nothing else) on stdout. */
export function parseModuleId(stdout: string): number {
  const trimmed = stdout.trim();
  if (!/^\d+$/.test(trimmed)) {
    throw new Error(`Could not parse a PulseAudio module id from pactl output: ${JSON.stringify(stdout)}`);
  }
  return Number.parseInt(trimmed, 10);
}

/**
 * Owns the null sink and the long-running `parecord` process for one meeting.
 * `setup()` before the browser launches, `start()` only after consent has been
 * announced, `stop()` then `teardown()` when the meeting ends.
 */
export class PulseAudio {
  readonly sinkName: string;
  private moduleId: number | null = null;
  private recording: ResultPromise | null = null;
  private outFile: string | null = null;

  constructor(sinkName: string) {
    this.sinkName = sinkName;
  }

  /** Load the null sink; returns its module id. */
  async setup(): Promise<number> {
    const { stdout } = await execa('pactl', buildLoadModuleArgs(this.sinkName));
    this.moduleId = parseModuleId(stdout);
    return this.moduleId;
  }

  /** Start recording the sink monitor into `outFile`. */
  async start(outFile: string): Promise<void> {
    if (this.recording) {
      throw new Error('Recorder already started');
    }
    this.outFile = outFile;
    this.recording = execa('parecord', buildParecordArgs({ sinkName: this.sinkName, outFile }), {
      stdio: 'ignore',
      reject: false,
    });
  }

  /** SIGINT the recorder and wait for it to flush the wav header. */
  async stop(): Promise<string | null> {
    const recording = this.recording;
    this.recording = null;
    if (!recording) {
      return this.outFile;
    }
    recording.kill('SIGINT');
    await recording.catch(() => undefined);
    return this.outFile;
  }

  /** Unload the null sink. Safe to call when setup never ran. */
  async teardown(): Promise<void> {
    await this.stop();
    if (this.moduleId === null) {
      return;
    }
    const moduleId = this.moduleId;
    this.moduleId = null;
    await execa('pactl', buildUnloadModuleArgs(moduleId)).catch(() => undefined);
  }

  /** True while a `parecord` process is running. */
  get isRecording(): boolean {
    return this.recording !== null;
  }
}
