import { createServer, type Server } from 'node:http';
import { readFile } from 'node:fs/promises';
import { AddressInfo } from 'node:net';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { chromium, type Browser, type Page } from 'playwright';
import { buildConsentMessage } from '../src/consent.js';
import { MeetPlatform } from '../src/platforms/meet.js';

const fixture = path.join(
  path.dirname(fileURLToPath(import.meta.url)),
  'fixtures',
  'fake-meet.html',
);

let server: Server;
let browser: Browser;
let page: Page;
let baseUrl: string;

const meet = new MeetPlatform();
const botName = 'Afterword Notetaker';

beforeAll(async () => {
  const html = await readFile(fixture, 'utf8');
  server = createServer((_req, res) => {
    res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
    res.end(html);
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  baseUrl = `http://127.0.0.1:${(server.address() as AddressInfo).port}/`;

  browser = await chromium.launch({ headless: true });
  page = await browser.newPage();
});

afterAll(async () => {
  await browser?.close();
  await new Promise<void>((resolve) => server.close(() => resolve()));
});

describe('MeetPlatform against a slow Meet page', () => {
  it('waits for join controls that only render after the page has loaded', async () => {
    const slowPage = await browser.newPage();
    try {
      await meet.join(slowPage, {
        botName,
        meetingUrl: `${baseUrl}?prejoinDelay=600`,
        admissionTimeoutMs: 20_000,
      });

      await expect(slowPage.locator('#joined-as').textContent()).resolves.toContain(botName);
      await expect(slowPage.locator('#mic').getAttribute('data-on')).resolves.toBe('false');
    } finally {
      await slowPage.close();
    }
  });
});

describe('MeetPlatform against the fake Meet page', () => {
  it('joins: dismisses the banner, mutes, fills the name and waits for admission', async () => {
    await meet.join(page, { botName, meetingUrl: baseUrl, admissionTimeoutMs: 20_000 });

    await expect(page.locator('#banner').isHidden()).resolves.toBe(true);
    await expect(page.locator('#mic').getAttribute('data-on')).resolves.toBe('false');
    await expect(page.locator('#camera').getAttribute('data-on')).resolves.toBe('false');
    await expect(page.locator('#joined-as').textContent()).resolves.toContain(botName);
    await expect(
      page.locator('button[aria-label*="Leave call" i]').isVisible(),
    ).resolves.toBe(true);
  });

  it('announces consent in the meeting chat before anything is recorded', async () => {
    const message = buildConsentMessage({
      botName,
      onBehalfOf: 'Jude Otine',
      privacyUrl: 'https://example.com/privacy',
    });

    await meet.announceConsent(page, message);

    await expect(page.locator('#chat-log .chat-message').first().textContent()).resolves.toBe(
      message,
    );
  });

  it('reads the participant count from the people badge', async () => {
    await expect(meet.participantCount(page)).resolves.toBe(3);
  });

  it('reports the meeting as live until the bot leaves', async () => {
    await expect(meet.isMeetingOver(page)).resolves.toBe(false);

    await meet.leave(page);

    await expect(meet.isMeetingOver(page)).resolves.toBe(true);
  });
});
