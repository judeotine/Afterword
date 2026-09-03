/**
 * Transcription via the `afterword-transcribe` Rust CLI — the same pipeline the
 * desktop app uses, so bot transcripts have the desktop's TranscriptSegment
 * shape (id, text, audio_start_time, audio_end_time, duration, display_time,
 * confidence, sequence_id).
 */
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

export interface TranscribeResult {
  transcriptPath: string;
}

/** Documented exit codes of the CLI. */
export const TRANSCRIBE_EXIT = {
  OK: 0,
  DECODE_FAILURE: 2,
  MODEL_MISSING: 3,
} as const;

/** `--input <wav> --out <dir> --engine <engine> --model <model> --models-dir <dir>` */
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

/** The CLI always writes its segments to `<outDir>/transcripts.json`. */
export function transcriptPathFor(outDir: string): string {
  return path.join(outDir, 'transcripts.json');
}

/** Human-readable reason for a non-zero exit code. */
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

export interface RunTranscribeInput {
  wav: string;
  outDir: string;
  /** Aborted when the job is cancelled; kills the CLI instead of waiting it out. */
  signal?: AbortSignal;
}

/** Run the CLI over a recorded wav and return where the transcript landed. */
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

  return { transcriptPath: transcriptPathFor(outDir) };
}
