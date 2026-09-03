import { describe, expect, it } from 'vitest';
import { buildConsentMessage } from '../src/consent.js';

const privacyUrl = 'https://example.com/privacy';

describe('buildConsentMessage', () => {
  it('names the bot, the user it acts for, and the privacy policy', () => {
    const message = buildConsentMessage({
      botName: 'Afterword Notetaker',
      onBehalfOf: 'Jude Otine',
      privacyUrl,
    });

    expect(message).toContain('Afterword Notetaker');
    expect(message).toContain('on behalf of Jude Otine');
    expect(message).toContain(privacyUrl);
    expect(message.toLowerCase()).toContain('recorded and transcribed');
  });

  it('tells participants how to object', () => {
    const message = buildConsentMessage({ botName: 'Bot', privacyUrl });
    expect(message.toLowerCase()).toContain('object');
    expect(message.toLowerCase()).toContain('remove the notetaker');
  });

  it('omits the "on behalf of" clause when no user is known', () => {
    const message = buildConsentMessage({ botName: 'Bot', privacyUrl });
    expect(message).not.toContain('on behalf of');
    expect(message).toContain('Bot');
  });

  it('is a single line so it can be sent as one chat message', () => {
    const message = buildConsentMessage({
      botName: 'Bot',
      onBehalfOf: 'Ada',
      privacyUrl,
    });
    expect(message).not.toContain('\n');
  });
});
