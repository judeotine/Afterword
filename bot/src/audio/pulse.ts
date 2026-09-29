import { execa, type ResultPromise } from 'execa';

export interface ParecordOptions {
  sinkName: string;
  outFile: string;
}

export function buildLoadModuleArgs(sinkName: string): string[] {
  return ['load-module', 'module-null-sink', `sink_name=${sinkName}`];
}

export function buildUnloadModuleArgs(moduleId: number): string[] {
  return ['unload-module', String(moduleId)];
}

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

export function parseModuleId(stdout: string): number {
  const trimmed = stdout.trim();
  if (!/^\d+$/.test(trimmed)) {
    throw new Error(`Could not parse a PulseAudio module id from pactl output: ${JSON.stringify(stdout)}`);
  }
  return Number.parseInt(trimmed, 10);
}

export class PulseAudio {
  readonly sinkName: string;
  private moduleId: number | null = null;
  private recording: ResultPromise | null = null;
  private outFile: string | null = null;

  constructor(sinkName: string) {
    this.sinkName = sinkName;
  }

  async setup(): Promise<number> {
    const { stdout } = await execa('pactl', buildLoadModuleArgs(this.sinkName));
    this.moduleId = parseModuleId(stdout);
    return this.moduleId;
  }

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

  async teardown(): Promise<void> {
    await this.stop();
    if (this.moduleId === null) {
      return;
    }
    const moduleId = this.moduleId;
    this.moduleId = null;
    await execa('pactl', buildUnloadModuleArgs(moduleId)).catch(() => undefined);
  }

  get isRecording(): boolean {
    return this.recording !== null;
  }
}
