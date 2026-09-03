/** Meeting platform adapters and URL detection. */
import type { Page } from 'playwright';
import { MeetPlatform } from './meet.js';
import { ZoomPlatform } from './zoom.js';
import { TeamsPlatform } from './teams.js';

export type PlatformName = 'meet' | 'zoom' | 'teams';

export interface JoinOptions {
  /** Display name the bot joins under. */
  botName: string;
  /** Meeting URL to open, when the page is not already on it. */
  meetingUrl?: string | undefined;
  /** How long to wait in the lobby before giving up (default 10 minutes). */
  admissionTimeoutMs?: number | undefined;
}

/**
 * One meeting platform. Every method drives a live Playwright page; none of
 * them own the browser, the recorder or the job lifecycle — that is worker.ts.
 */
export interface MeetingPlatform {
  readonly name: PlatformName;
  join(page: Page, opts: JoinOptions): Promise<void>;
  announceConsent(page: Page, text: string): Promise<void>;
  participantCount(page: Page): Promise<number>;
  isMeetingOver(page: Page): Promise<boolean>;
  leave(page: Page): Promise<void>;
}

/** Platforms whose URLs the bot recognises. */
export const RECOGNISED_PLATFORMS: readonly PlatformName[] = ['meet', 'zoom', 'teams'];

/** Platforms the bot can actually attend today. */
export const SUPPORTED_PLATFORMS: readonly PlatformName[] = ['meet'];

function toUrl(raw: string): URL | null {
  const trimmed = raw.trim();
  if (!trimmed || /\s/.test(trimmed)) {
    return null;
  }
  try {
    return new URL(/^https?:\/\//i.test(trimmed) ? trimmed : `https://${trimmed}`);
  } catch {
    return null;
  }
}

/** meet.google.com/xxx-xxxx-xxx */
function isMeetUrl(url: URL): boolean {
  const host = url.hostname.toLowerCase();
  if (host !== 'meet.google.com' && host !== 'www.meet.google.com') {
    return false;
  }
  return /^\/[a-z]{3}-[a-z]{4}-[a-z]{3}\/?$/i.test(url.pathname);
}

/** zoom.us/j/<id> (also personal/vanity subdomains and /wc /s join links) */
function isZoomUrl(url: URL): boolean {
  const host = url.hostname.toLowerCase();
  if (host !== 'zoom.us' && !host.endsWith('.zoom.us')) {
    return false;
  }
  return /^\/(j|s|wc)\//i.test(url.pathname);
}

/** teams.microsoft.com/l/meetup-join/... */
function isTeamsUrl(url: URL): boolean {
  const host = url.hostname.toLowerCase();
  if (host !== 'teams.microsoft.com' && host !== 'teams.live.com') {
    return false;
  }
  return url.pathname.toLowerCase().includes('/l/meetup-join');
}

/**
 * Map a meeting URL onto its adapter. Zoom and Teams are recognised but their
 * adapters throw NotImplementedError, so the API can answer "recognised but not
 * yet supported" instead of a flat 400.
 */
export function detectPlatform(meetingUrl: string): MeetingPlatform | null {
  const url = toUrl(meetingUrl);
  if (!url) {
    return null;
  }
  if (isMeetUrl(url)) {
    return new MeetPlatform();
  }
  if (isZoomUrl(url)) {
    return new ZoomPlatform();
  }
  if (isTeamsUrl(url)) {
    return new TeamsPlatform();
  }
  return null;
}
