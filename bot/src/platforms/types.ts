import type { Page } from 'playwright';
import { MeetPlatform } from './meet.js';
import { ZoomPlatform } from './zoom.js';
import { TeamsPlatform } from './teams.js';

export type PlatformName = 'meet' | 'zoom' | 'teams';

export interface JoinOptions {
  botName: string;
  meetingUrl?: string | undefined;
  admissionTimeoutMs?: number | undefined;
  signal?: AbortSignal | undefined;
}

export interface MeetingPlatform {
  readonly name: PlatformName;
  join(page: Page, opts: JoinOptions): Promise<void>;
  announceConsent(page: Page, text: string): Promise<void>;
  participantCount(page: Page): Promise<number>;
  isMeetingOver(page: Page): Promise<boolean>;
  leave(page: Page): Promise<void>;
}

export const RECOGNISED_PLATFORMS: readonly PlatformName[] = ['meet', 'zoom', 'teams'];

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

function isMeetUrl(url: URL): boolean {
  const host = url.hostname.toLowerCase();
  if (host !== 'meet.google.com' && host !== 'www.meet.google.com') {
    return false;
  }
  return /^\/[a-z]{3}-[a-z]{4}-[a-z]{3}\/?$/i.test(url.pathname);
}

function isZoomUrl(url: URL): boolean {
  const host = url.hostname.toLowerCase();
  if (host !== 'zoom.us' && !host.endsWith('.zoom.us')) {
    return false;
  }
  return /^\/(j|s|wc)\//i.test(url.pathname);
}

function isTeamsUrl(url: URL): boolean {
  const host = url.hostname.toLowerCase();
  if (host !== 'teams.microsoft.com' && host !== 'teams.live.com') {
    return false;
  }
  return url.pathname.toLowerCase().includes('/l/meetup-join');
}

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
