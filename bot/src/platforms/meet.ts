import type { Locator, Page } from 'playwright';
import { AdmissionTimeoutError } from '../errors.js';
import type { JoinOptions, MeetingPlatform, PlatformName } from './types.js';

export const MEET_SELECTORS = {
  dismissButtons: ['Got it', 'Dismiss', 'Continue without microphone and camera', 'No thanks'],
  nameInput: [
    'input[aria-label="Your name"]',
    'input[placeholder="Your name"]',
    'input[aria-label*="name" i]',
  ],
  micOff: ['button[aria-label*="Turn off microphone" i]', 'div[role="button"][aria-label*="Turn off microphone" i]'],
  cameraOff: ['button[aria-label*="Turn off camera" i]', 'div[role="button"][aria-label*="Turn off camera" i]'],
  joinButtons: ['Ask to join', 'Join now', 'Join anyway'],
  leaveButton: [
    'button[aria-label*="Leave call" i]',
    'div[role="button"][aria-label*="Leave call" i]',
  ],
  chatToggle: [
    'button[aria-label*="Chat with everyone" i]',
    'div[role="button"][aria-label*="Chat with everyone" i]',
  ],
  chatInput: [
    'textarea[aria-label*="Send a message" i]',
    'input[aria-label*="Send a message" i]',
    '[contenteditable="true"][aria-label*="Send a message" i]',
  ],
  participantBadge: [
    'button[aria-label*="participant" i]',
    'div[role="button"][aria-label*="participant" i]',
    '[aria-label*="participant" i]',
  ],
  videoTile: ['[data-participant-id]', '[data-requested-participant-id]'],
  endedTexts: [
    "You've been removed",
    'You have been removed',
    'The call ended',
    'Return to home screen',
    'You left the meeting',
  ],
} as const;

const SHORT_TIMEOUT_MS = 5_000;
const PROBE_TIMEOUT_MS = 1_000;
const PREJOIN_TIMEOUT_MS = 30_000;
const ADMISSION_POLL_MS = 500;
const DEFAULT_ADMISSION_TIMEOUT_MS = 10 * 60_000;
export const UNKNOWN_PARTICIPANT_COUNT = 2;

async function firstVisible(
  page: Page,
  selectors: readonly string[],
  timeoutMs: number = PROBE_TIMEOUT_MS,
): Promise<Locator | null> {
  const locator = page
    .locator(selectors.join(', '))
    .filter({ visible: true })
    .first();
  try {
    await locator.waitFor({ state: 'visible', timeout: timeoutMs });
    return locator;
  } catch {
    return null;
  }
}

async function firstVisibleButton(
  page: Page,
  names: readonly string[],
  timeoutMs: number = PROBE_TIMEOUT_MS,
): Promise<Locator | null> {
  for (const name of names) {
    const locator = page
      .getByRole('button', { name, exact: false })
      .or(page.locator(`button:has-text("${name}"), div[role="button"]:has-text("${name}")`))
      .filter({ visible: true })
      .first();
    try {
      await locator.waitFor({ state: 'visible', timeout: timeoutMs });
      return locator;
    } catch {
      // Not this one: try the next candidate.
    }
  }
  return null;
}

function escapeForRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

function parseCount(text: string | null): number | null {
  if (!text) {
    return null;
  }
  const match = text.match(/\d+/);
  return match ? Number.parseInt(match[0], 10) : null;
}

export class MeetPlatform implements MeetingPlatform {
  readonly name: PlatformName = 'meet';

  async join(page: Page, opts: JoinOptions): Promise<void> {
    const admissionTimeoutMs = opts.admissionTimeoutMs ?? DEFAULT_ADMISSION_TIMEOUT_MS;

    if (opts.meetingUrl && page.url() !== opts.meetingUrl) {
      await page.goto(opts.meetingUrl, { waitUntil: 'domcontentloaded' });
    }

    const joinButton = await firstVisibleButton(
      page,
      MEET_SELECTORS.joinButtons,
      PREJOIN_TIMEOUT_MS,
    );
    if (!joinButton) {
      throw new Error(
        `Could not find a join button on the Meet page (tried: ${MEET_SELECTORS.joinButtons.join(', ')})`,
      );
    }

    await this.dismissPrompts(page);
    await this.turnOffDevices(page);

    const nameBox = await firstVisible(page, MEET_SELECTORS.nameInput);
    if (nameBox) {
      await nameBox.fill(opts.botName);
    }

    await joinButton.click();

    const admitted = await this.waitForAdmission(page, admissionTimeoutMs, opts.signal);
    if (!admitted) {
      throw new AdmissionTimeoutError(admissionTimeoutMs);
    }
  }

  async announceConsent(page: Page, text: string): Promise<void> {
    let input = await firstVisible(page, MEET_SELECTORS.chatInput);
    if (!input) {
      const toggle = await firstVisible(page, MEET_SELECTORS.chatToggle, SHORT_TIMEOUT_MS);
      if (!toggle) {
        throw new Error('Could not open the Meet chat panel to announce consent');
      }
      await toggle.click();
      input = await firstVisible(page, MEET_SELECTORS.chatInput, SHORT_TIMEOUT_MS);
    }
    if (!input) {
      throw new Error('Could not find the Meet chat message box to announce consent');
    }

    await input.click();
    await input.fill(text);
    await input.press('Enter');
  }

  async participantCount(page: Page): Promise<number> {
    const badge = await firstVisible(page, MEET_SELECTORS.participantBadge);
    if (badge) {
      const fromLabel = parseCount(await badge.getAttribute('aria-label'));
      if (fromLabel !== null) {
        return fromLabel;
      }
      const fromText = parseCount((await badge.textContent())?.trim() ?? null);
      if (fromText !== null) {
        return fromText;
      }
    }

    for (const selector of MEET_SELECTORS.videoTile) {
      const tiles = await page.locator(selector).count().catch(() => 0);
      if (tiles > 0) {
        return tiles;
      }
    }

    return UNKNOWN_PARTICIPANT_COUNT;
  }

  async isMeetingOver(page: Page): Promise<boolean> {
    if (page.isClosed()) {
      return true;
    }

    try {
      const host = new URL(page.url()).hostname.toLowerCase();
      if (!host.endsWith('meet.google.com') && host !== 'localhost' && host !== '127.0.0.1') {
        return true;
      }
    } catch {
      return true;
    }

    const ended = page
      .getByText(new RegExp(MEET_SELECTORS.endedTexts.map(escapeForRegExp).join('|'), 'i'))
      .filter({ visible: true })
      .first();
    return await ended
      .waitFor({ state: 'visible', timeout: 250 })
      .then(() => true)
      .catch(() => false);
  }

  async leave(page: Page): Promise<void> {
    if (page.isClosed()) {
      return;
    }
    const button = await firstVisible(page, MEET_SELECTORS.leaveButton);
    if (button) {
      await button.click({ timeout: SHORT_TIMEOUT_MS }).catch(() => undefined);
    }
  }

  private async dismissPrompts(page: Page): Promise<void> {
    for (const name of MEET_SELECTORS.dismissButtons) {
      const button = await firstVisibleButton(page, [name]);
      if (button) {
        await button.click({ timeout: SHORT_TIMEOUT_MS }).catch(() => undefined);
      }
    }
  }

  private async turnOffDevices(page: Page): Promise<void> {
    for (const selectors of [MEET_SELECTORS.micOff, MEET_SELECTORS.cameraOff]) {
      const toggle = await firstVisible(page, selectors);
      if (toggle) {
        await toggle.click({ timeout: SHORT_TIMEOUT_MS }).catch(() => undefined);
      }
    }
  }

  private async waitForAdmission(
    page: Page,
    timeoutMs: number,
    signal?: AbortSignal,
  ): Promise<boolean> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      if (signal?.aborted) {
        throw new Error('Cancelled while waiting to be admitted to the meeting');
      }
      const leaveButton = await firstVisible(page, MEET_SELECTORS.leaveButton, ADMISSION_POLL_MS);
      if (leaveButton) {
        return true;
      }
      if (await this.isMeetingOver(page)) {
        return false;
      }
    }
    return false;
  }
}
