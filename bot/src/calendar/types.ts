
export interface UpcomingMeeting {
  id: string;
  title: string;
  startAt: string;
  meetingUrl: string;
}

export interface CalendarSource {
  listUpcoming(windowMinutes: number): Promise<UpcomingMeeting[]>;
}
