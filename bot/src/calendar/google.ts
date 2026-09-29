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
