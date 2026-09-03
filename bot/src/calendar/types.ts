/** Calendar sources that can feed the bot upcoming meetings (Phase 4). */

export interface UpcomingMeeting {
  /** Stable id from the calendar provider. */
  id: string;
  title: string;
  /** ISO 8601 start time. */
  startAt: string;
  /** Meeting URL parsed out of the event (location / conferencing data). */
  meetingUrl: string;
}

export interface CalendarSource {
  /** Events starting within the next `windowMinutes` that carry a meeting URL. */
  listUpcoming(windowMinutes: number): Promise<UpcomingMeeting[]>;
}
