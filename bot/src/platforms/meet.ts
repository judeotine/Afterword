/**
 * Google Meet adapter.
 *
 * Meet's DOM changes often, so every selector lives in MEET_SELECTORS below and
 * each step tries a list of candidates (role/aria-label/text) before giving up.
 * Steps that are merely nice to have (dismissing a banner, muting) never throw.
 */
import type { Locator, Page } from 'playwright';
import { AdmissionTimeoutError } from '../errors.js';
import type { JoinOptions, MeetingPlatform, PlatformName } from './types.js';

/** Every Meet selector the bot depends on, in one place. */
export const MEET_SELECTORS = {
  /** Consent/onboarding banners to clear before the pre-join screen is usable. */
  dismissButtons: ['Got it', 'Dismiss', 'Continue without microphone and camera', 'No thanks'],
  /** "Your name" box on the pre-join screen. */
  nameInput: [
    'input[aria-label="Your name"]',
    'input[placeholder="Your name"]',
    'input[aria-label*="name" i]',
  ],
  /** Pre-join mic/camera toggles: present only when they are still on. */
  micOff: ['button[aria-label*="Turn off microphone" i]', 'div[role="button"][aria-label*="Turn off microphone" i]'],
  cameraOff: ['button[aria-label*="Turn off camera" i]', 'div[role="button"][aria-label*="Turn off camera" i]'],
  /** Join buttons, most-specific first. */
  joinButtons: ['Ask to join', 'Join now', 'Join anyway'],
  /** Visible only once the bot is actually in the call. */
  leaveButton: [
    'button[aria-label*="Leave call" i]',
    'div[role="button"][aria-label*="Leave call" i]',
  ],
  /** Chat panel used for the consent announcement. */
  chatToggle: [
    'button[aria-label*="Chat with everyone" i]',
    'div[role="button"][aria-label*="Chat with everyone" i]',
  ],
  chatInput: [
    'textarea[aria-label*="Send a message" i]',
    'input[aria-label*="Send a message" i]',
    '[contenteditable="true"][aria-label*="Send a message" i]',
  ],
  /** People count badge; falls back to counting video tiles. */
  participantBadge: [
    'button[aria-label*="participant" i]',
    'div[role="button"][aria-label*="participant" i]',
    '[aria-label*="participant" i]',
  ],
  videoTile: ['[data-participant-id]', '[data-requested-participant-id]'],
  /** Phrases Meet shows when the call is over for the bot. */
  endedTexts: [
    "You've been removed",
    'You have been removed',
    'The call ended',
    'Return to home screen',
    'You left the meeting',
  ],
} as const;

const SHORT_TIMEOUT_MS = 5_000;
/** Probe budget for optional controls (banners, mute buttons) that may not exist. */
const PROBE_TIMEOUT_MS = 1_000;
/** Budget for controls the Meet SPA renders some time after domcontentloaded. */
const PREJOIN_TIMEOUT_MS = 30_000;
/** How often the admission wait re-checks the page and the cancellation signal. */
const ADMISSION_POLL_MS = 500;
const DEFAULT_ADMISSION_TIMEOUT_MS = 10 * 60_000;
/**
 * Reported when neither the badge nor the tiles can be read. It must never be
 * <= 1, otherwise a selector break would look like "the bot is alone" and cut
 * the recording short; MAX_MEETING_MINUTES remains the backstop.
 */
export const UNKNOWN_PARTICIPANT_COUNT = 2;

/**
 * Wait for the first of `selectors` to become visible.
 *
 * `locator.isVisible()` is a point-in-time check — its `timeout` option is
 * deprecated and ignored — so it would return false on a Meet page that has
 * only just fired domcontentloaded. `waitFor({ state: 'visible' })` actually
 * waits, and `.filter({ visible: true })` keeps the wait honest when a hidden
 * element matches the selector earlier in DOM order.
 */
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

/** Same, for buttons matched by accessible name (aria-label or text). */
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

  /** Open the meeting, mute everything, set the bot's name and wait for admission. */
  async join(page: Page, opts: JoinOptions): Promise<void> {
    const admissionTimeoutMs = opts.admissionTimeoutMs ?? DEFAULT_ADMISSION_TIMEOUT_MS;

    if (opts.meetingUrl && page.url() !== opts.meetingUrl) {
      await page.goto(opts.meetingUrl, { waitUntil: 'domcontentloaded' });
    }

    // Wait for the pre-join screen itself before probing the optional controls,
    // otherwise every short probe below races the SPA's first render.
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

  /** Post the consent notice in the meeting chat. Called before recording starts. */
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

  /** People count from the badge, else the number of video tiles. */
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

  /** True when the bot was removed, the call ended, or Meet navigated away. */
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

  /** Click "Leave call"; never throws, the browser is closed either way. */
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

  /**
   * Poll for the in-call UI. The loop is short so a cancellation (SIGTERM while
   * the bot sits in the lobby) is noticed in well under a second rather than
   * after the full admission timeout.
   */
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
