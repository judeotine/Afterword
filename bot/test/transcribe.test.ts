import { describe, expect, it } from 'vitest';
import {
  buildTranscribeArgs,
  describeTranscribeFailure,
  parseTranscribeSummary,
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

describe('parseTranscribeSummary', () => {
  it('parses the CLI summary line', () => {
    const stdout =
      '{"transcript":"/out/transcripts.json","metadata":"/out/metadata.json","segments":12,"duration_seconds":345.6}';
    expect(parseTranscribeSummary(stdout)).toEqual({ segments: 12, durationSeconds: 345.6 });
  });

  it('tolerates trailing blank lines after the summary', () => {
    const stdout =
      '{"transcript":"/out/transcripts.json","metadata":"/out/metadata.json","segments":3,"duration_seconds":10}\n\n  \n';
    expect(parseTranscribeSummary(stdout)).toEqual({ segments: 3, durationSeconds: 10 });
  });

  it('throws on a malformed summary line', () => {
    expect(() => parseTranscribeSummary('not json')).toThrow(/malformed/i);
  });

  it('throws when the summary line is missing expected fields', () => {
    expect(() => parseTranscribeSummary('{"transcript":"/out/transcripts.json"}')).toThrow(
      /summary/i,
    );
  });

  it('throws on empty stdout', () => {
    expect(() => parseTranscribeSummary('')).toThrow(/summary/i);
  });
});
