import { describe, expect, it } from 'vitest';
import { detectPlatform } from '../src/platforms/types.js';

describe('detectPlatform', () => {
  it('recognises a Google Meet meeting URL', () => {
    const platform = detectPlatform('https://meet.google.com/abc-defg-hij');
    expect(platform?.name).toBe('meet');
  });

  it('recognises a Meet URL with query parameters and no scheme', () => {
    expect(detectPlatform('meet.google.com/abc-defg-hij?authuser=0')?.name).toBe('meet');
  });

  it('rejects a Meet landing page without a meeting code', () => {
    expect(detectPlatform('https://meet.google.com/')).toBeNull();
    expect(detectPlatform('https://meet.google.com/landing')).toBeNull();
  });

  it('recognises Zoom join URLs', () => {
    expect(detectPlatform('https://zoom.us/j/1234567890')?.name).toBe('zoom');
    expect(detectPlatform('https://acme.zoom.us/j/1234567890?pwd=xyz')?.name).toBe('zoom');
  });

  it('recognises Teams meetup-join URLs', () => {
    expect(
      detectPlatform('https://teams.microsoft.com/l/meetup-join/19%3ameeting_abc%40thread.v2/0')?.name,
    ).toBe('teams');
  });

  it('returns null for unrelated or malformed URLs', () => {
    expect(detectPlatform('https://example.com/whatever')).toBeNull();
    expect(detectPlatform('not a url')).toBeNull();
    expect(detectPlatform('')).toBeNull();
  });

  it('returns adapters whose unimplemented platforms throw NotImplementedError', async () => {
    const zoom = detectPlatform('https://zoom.us/j/1234567890');
    expect(zoom).not.toBeNull();
    await expect(zoom!.join({} as never, { botName: 'Afterword Notetaker' })).rejects.toThrow(
      /zoom/i,
    );
  });
});
