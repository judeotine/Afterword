import { describe, expect, it } from 'vitest';
import {
  buildTranscribeArgs,
  describeTranscribeFailure,
  transcriptPathFor,
} from '../src/transcribe.js';

describe('buildTranscribeArgs', () => {
  it('matches the afterword-transcribe CLI flag contract', () => {
    expect(
      buildTranscribeArgs({
        wav: '/rec/meeting.wav',
        outDir: '/rec/meeting',
        engine: 'whisper',
        model: 'base',
        modelsDir: '/models',
      }),
    ).toEqual([
      '--input',
      '/rec/meeting.wav',
      '--out',
      '/rec/meeting',
      '--engine',
      'whisper',
      '--model',
      'base',
      '--models-dir',
      '/models',
    ]);
  });

  it('passes the parakeet engine through unchanged', () => {
    const args = buildTranscribeArgs({
      wav: '/a.wav',
      outDir: '/out',
      engine: 'parakeet',
      model: 'parakeet-v2',
      modelsDir: '/m',
    });
    expect(args).toContain('parakeet');
    expect(args[args.indexOf('--engine') + 1]).toBe('parakeet');
  });
});

describe('transcriptPathFor', () => {
  it('points at transcripts.json inside the output directory', () => {
    expect(transcriptPathFor('/rec/meeting')).toBe('/rec/meeting/transcripts.json');
  });
});

describe('describeTranscribeFailure', () => {
  it('explains the documented non-zero exit codes', () => {
    expect(describeTranscribeFailure(2)).toMatch(/decode/i);
    expect(describeTranscribeFailure(3)).toMatch(/model/i);
    expect(describeTranscribeFailure(1)).toMatch(/exit code 1/i);
  });
});
