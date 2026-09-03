/**
 * Google Calendar source — stub.
 *
 * Phase 4 of the roadmap adds the OAuth flow (per-user refresh token stored by
 * the backend) plus a poller that turns "meeting starts in <n> minutes" into a
 * scheduled job here. Nothing in this file may touch the network until that
 * consent and token-storage design is reviewed.
 */
import { NotImplementedError } from '../errors.js';
import type { CalendarSource, UpcomingMeeting } from './types.js';

export class GoogleCalendarSource implements CalendarSource {
  async listUpcoming(_windowMinutes: number): Promise<UpcomingMeeting[]> {
    throw new NotImplementedError(
      'google-calendar',
      'Google Calendar polling arrives in Phase 4 (OAuth + polling); see ROADMAP.md',
    );
  }
}
