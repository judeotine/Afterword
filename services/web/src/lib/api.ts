export interface ApiErrorBody {
  error?: { code?: string; message?: string };
}

export class ApiError extends Error {
  status: number;
  code: string;
  constructor(message: string, status: number, code: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

export interface Meeting {
  id: string;
  title: string;
  status: string;
  started_at: string | null;
  duration_seconds: number | null;
  visibility: string;
}

export interface TranscriptSegment {
  id: string;
  speaker: string | null;
  start_ms: number;
  end_ms: number;
  text: string;
}

export interface Summary {
  id: string;
  format: string;
  content: string;
  created_at: string;
}

export interface AskCitation {
  segment_id: string;
  start_ms: number;
  text: string;
}

export interface AskAnswer {
  answer: string;
  citations: AskCitation[];
}

export interface Page<T> {
  items: T[];
  next_cursor: string | null;
}

export interface BotJob {
  id: string;
  workspace_id: string;
  meeting_url: string;
  platform: string;
  status: string;
  scheduled_at: string;
  estimated_minutes: number;
  minutes_used: number;
  created_at: string;
}

export interface RequestOptions {
  accessToken?: string;
  workspaceId?: string;
  signal?: AbortSignal;
}

export function apiBaseUrl(): string {
  return process.env.NEXT_PUBLIC_API_BASE_URL ?? 'http://localhost:8080';
}

export class ApiClient {
  readonly baseUrl: string;
  private readonly fetchImpl: typeof fetch;

  constructor(baseUrl: string = apiBaseUrl(), fetchImpl: typeof fetch = fetch) {
    this.baseUrl = baseUrl.replace(/\/+$/, '');
    this.fetchImpl = fetchImpl;
  }

  googleStartUrl(redirectTo: string): string {
    return `${this.baseUrl}/v1/auth/google/start?redirect_to=${encodeURIComponent(redirectTo)}`;
  }

  refresh(refreshToken: string): Promise<{ access_token: string; refresh_token: string }> {
    return this.request('POST', '/v1/auth/refresh', { body: { refresh_token: refreshToken } });
  }

  listWorkspaces(options: RequestOptions): Promise<{ workspaces: { id: string; name: string; role: string }[] }> {
    return this.request('GET', '/v1/workspaces', options);
  }

  listMeetings(cursor: string | null, options: RequestOptions): Promise<Page<Meeting>> {
    const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
    return this.request('GET', `/v1/meetings${query}`, options);
  }

  getMeeting(meetingId: string, options: RequestOptions): Promise<Meeting> {
    return this.request('GET', `/v1/meetings/${meetingId}`, options);
  }

  listSegments(meetingId: string, options: RequestOptions): Promise<Page<TranscriptSegment>> {
    return this.request('GET', `/v1/meetings/${meetingId}/segments`, options);
  }

  getSummary(meetingId: string, options: RequestOptions): Promise<Summary> {
    return this.request('GET', `/v1/meetings/${meetingId}/summary`, options);
  }

  ask(meetingId: string, question: string, options: RequestOptions): Promise<AskAnswer> {
    return this.request('POST', `/v1/meetings/${meetingId}/ask`, { ...options, body: { question } });
  }

  sendBot(meetingUrl: string, options: RequestOptions, extras?: { bot_name?: string; scheduled_at?: string }): Promise<BotJob> {
    return this.request('POST', '/v1/bot-jobs', {
      ...options,
      body: { meeting_url: meetingUrl, ...extras },
    });
  }

  async listBotJobs(options: RequestOptions): Promise<Page<BotJob>> {
    const raw = await this.request<{ bot_jobs?: BotJob[]; next_cursor?: string | null }>('GET', '/v1/bot-jobs', options);
    return { items: raw.bot_jobs ?? [], next_cursor: raw.next_cursor ?? null };
  }

  getSharedMeeting(token: string): Promise<{ meeting: Meeting; segments: TranscriptSegment[]; summary: Summary | null }> {
    return this.request('GET', `/v1/share/${token}`, {});
  }

  private async request<T>(
    method: string,
    path: string,
    options: RequestOptions & { body?: unknown },
  ): Promise<T> {
    const headers: Record<string, string> = { 'content-type': 'application/json' };
    if (options.accessToken) {
      headers['authorization'] = `Bearer ${options.accessToken}`;
    }
    if (options.workspaceId) {
      headers['x-workspace-id'] = options.workspaceId;
    }
    const response = await this.fetchImpl(`${this.baseUrl}${path}`, {
      method,
      headers,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      signal: options.signal,
      cache: 'no-store',
    });
    if (!response.ok) {
      let message = `request failed with status ${response.status}`;
      let code = 'request_failed';
      try {
        const body = (await response.json()) as ApiErrorBody;
        if (body.error?.message) {
          message = body.error.message;
        }
        if (body.error?.code) {
          code = body.error.code;
        }
      } catch {
        // no JSON body
      }
      throw new ApiError(message, response.status, code);
    }
    if (response.status === 204) {
      return undefined as T;
    }
    const text = await response.text();
    if (!text) {
      return undefined as T;
    }
    return JSON.parse(text) as T;
  }
}
