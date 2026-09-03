/**
 * Environment validation.
 *
 * PRIVACY_URL has no default on purpose: the consent notice announces that link
 * to every participant, so a deployment must supply one that actually describes
 * it rather than inheriting Afterword's own policy.
 */
import { describe, expect, it } from 'vitest';
import { loadConfig } from '../src/config.js';

const minimalEnv = { PRIVACY_URL: 'https://example.test/privacy' };

describe('loadConfig', () => {
  it('refuses to start without PRIVACY_URL', () => {
    expect(() => loadConfig({})).toThrow(/PRIVACY_URL/);
  });

  it('refuses a PRIVACY_URL that is not a url', () => {
    expect(() => loadConfig({ PRIVACY_URL: 'see the wiki' })).toThrow(/PRIVACY_URL/);
  });

  it('accepts a url and fills the remaining defaults', () => {
    const config = loadConfig(minimalEnv);
    expect(config.PRIVACY_URL).toBe('https://example.test/privacy');
    expect(config.PORT).toBe(8787);
    expect(config.BOT_NAME).toBe('Afterword Notetaker');
  });
});
