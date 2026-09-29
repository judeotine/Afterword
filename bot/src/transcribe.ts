import path from 'node:path';
import { execa } from 'execa';
import { config, type BotConfig } from './config.js';

export interface TranscribeArgsInput {
  wav: string;
  outDir: string;
  engine: BotConfig['TRANSCRIBE_ENGINE'];
  model: string;
  modelsDir: string;
}

export interface TranscriptSegment {
  id: string;
  text: string;
  timestamp: string;
  audio_start_time?: number;
  audio_end_time?: number;
  duration?: number;
}

export interface TranscribeResult {
  transcriptPath: string;
  segments: number;
  durationSeconds: number;
}

export const TRANSCRIBE_EXIT = {
  OK: 0,
  DECODE_FAILURE: 2,
  MODEL_MISSING: 3,
} as const;

export function buildTranscribeArgs({
  wav,
  outDir,
  engine,
  model,
  modelsDir,
}: TranscribeArgsInput): string[] {
  return [
    '--input',
    wav,
    '--out',
    outDir,
    '--engine',
    engine,
    '--model',
    model,
    '--models-dir',
    modelsDir,
  ];
}

export function transcriptPathFor(outDir: string): string {
  return path.join(outDir, 'transcripts.json');
}

export function describeTranscribeFailure(code: number): string {
  switch (code) {
    case TRANSCRIBE_EXIT.DECODE_FAILURE:
      return 'afterword-transcribe could not decode the recording (exit code 2)';
    case TRANSCRIBE_EXIT.MODEL_MISSING:
      return 'afterword-transcribe could not find the requested model (exit code 3)';
    default:
      return `afterword-transcribe failed with exit code ${code}`;
  }
}

export interface TranscribeSummary {
  segments: number;
  durationSeconds: number;
}

export function parseTranscribeSummary(stdout: string): TranscribeSummary {
  const lines = stdout
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
  const lastLine = lines[lines.length - 1];
  if (!lastLine) {
    throw new Error('afterword-transcribe produced no summary line on stdout');
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(lastLine);
  } catch {
    throw new Error(`afterword-transcribe produced a malformed summary line: ${lastLine}`);
  }

  if (
    typeof parsed !== 'object' ||
    parsed === null ||
    typeof (parsed as Record<string, unknown>).segments !== 'number' ||
    typeof (parsed as Record<string, unknown>).duration_seconds !== 'number'
  ) {
    throw new Error(`afterword-transcribe summary line is missing expected fields: ${lastLine}`);
  }

  const summary = parsed as { segments: number; duration_seconds: number };
  return { segments: summary.segments, durationSeconds: summary.duration_seconds };
}

export interface RunTranscribeInput {
  wav: string;
  outDir: string;
  signal?: AbortSignal;
}

export async function runTranscribe(
  { wav, outDir, signal }: RunTranscribeInput,
  settings: BotConfig = config,
): Promise<TranscribeResult> {
  const args = buildTranscribeArgs({
    wav,
    outDir,
    engine: settings.TRANSCRIBE_ENGINE,
    model: settings.TRANSCRIBE_MODEL,
    modelsDir: settings.MODELS_DIR,
  });

  const result = await execa(settings.TRANSCRIBE_BIN, args, {
    reject: false,
    ...(signal ? { cancelSignal: signal } : {}),
  });
  const code = typeof result.exitCode === 'number' ? result.exitCode : 1;
  if (code !== TRANSCRIBE_EXIT.OK) {
    const stderr = typeof result.stderr === 'string' ? result.stderr.trim() : '';
    throw new Error(
      stderr ? `${describeTranscribeFailure(code)}: ${stderr}` : describeTranscribeFailure(code),
    );
  }

  const stdout = typeof result.stdout === 'string' ? result.stdout : '';
  const summary = parseTranscribeSummary(stdout);
  return {
    transcriptPath: transcriptPathFor(outDir),
    segments: summary.segments,
    durationSeconds: summary.durationSeconds,
  };
}
