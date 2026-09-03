import { describe, expect, it } from 'vitest';
import {
  buildLoadModuleArgs,
  buildParecordArgs,
  buildUnloadModuleArgs,
  parseModuleId,
} from '../src/audio/pulse.js';

describe('pulse command builders', () => {
  it('builds the null-sink load-module arguments', () => {
    expect(buildLoadModuleArgs('afterword_sink')).toEqual([
      'load-module',
      'module-null-sink',
      'sink_name=afterword_sink',
    ]);
  });

  it('builds parecord arguments recording the sink monitor as 16 kHz mono s16le wav', () => {
    expect(buildParecordArgs({ sinkName: 'afterword_sink', outFile: '/tmp/a.wav' })).toEqual([
      '--device=afterword_sink.monitor',
      '--file-format=wav',
      '--channels=1',
      '--rate=16000',
      '--format=s16le',
      '/tmp/a.wav',
    ]);
  });

  it('builds unload-module arguments', () => {
    expect(buildUnloadModuleArgs(27)).toEqual(['unload-module', '27']);
  });
});

describe('parseModuleId', () => {
  it('parses the module id pactl prints on stdout', () => {
    expect(parseModuleId('27\n')).toBe(27);
    expect(parseModuleId('  536870912  ')).toBe(536870912);
  });

  it('throws when pactl printed something unexpected', () => {
    expect(() => parseModuleId('')).toThrow();
    expect(() => parseModuleId('Failure: Module initialization failed')).toThrow();
  });
});
