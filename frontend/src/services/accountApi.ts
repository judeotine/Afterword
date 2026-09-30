'use client';

export interface AccountSession {
  accessToken: string;
  refreshToken: string;
  workspaceId: string;
  workspaceName: string;
}

export interface WorkspaceSummary {
  id: string;
  name: string;
  role: string;
}

export interface BalanceSnapshot {
  credits: number;
}

export interface BotJobSummary {
  id: string;
  meeting_url: string;
  platform: string;
  status: string;
  created_at: string;
}

export class AccountApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.name = 'AccountApiError';
    this.status = status;
  }
}

export interface AccountApiOptions {
  baseUrl: string;
  fetchImpl?: typeof fetch;
}

export class AccountApi {
  private readonly baseUrl: string;
  private readonly fetchImpl: typeof fetch;

  constructor(options: AccountApiOptions) {
    this.baseUrl = options.baseUrl.replace(/\/+$/, '');
    this.fetchImpl = options.fetchImpl ?? fetch;
  }

  async refresh(refreshToken: string): Promise<{ access_token: string; refresh_token: string }> {
    return this.post('/v1/auth/refresh', { refresh_token: refreshToken });
  }

  async listWorkspaces(accessToken: string): Promise<{ workspaces: WorkspaceSummary[] }> {
    return this.get('/v1/workspaces', accessToken);
  }

  async balance(accessToken: string, workspaceId: string): Promise<BalanceSnapshot> {
    const raw = await this.get<{ minutes?: number }>('/v1/billing/balance', accessToken, workspaceId);
    return { credits: raw.minutes ?? 0 };
  }

  async sendBot(accessToken: string, workspaceId: string, meetingUrl: string, botName?: string): Promise<BotJobSummary> {
    const body: Record<string, string> = { meeting_url: meetingUrl };
    if (botName && botName.trim()) {
      body.bot_name = botName.trim();
    }
    return this.post('/v1/bot-jobs', body, accessToken, workspaceId);
  }

  async listBotJobs(accessToken: string, workspaceId: string): Promise<{ items: BotJobSummary[] }> {
    const raw = await this.get<{ bot_jobs?: BotJobSummary[] }>('/v1/bot-jobs', accessToken, workspaceId);
    return { items: raw.bot_jobs ?? [] };
  }

  private async post<T>(path: string, body: unknown, accessToken?: string, workspaceId?: string): Promise<T> {
    const response = await this.fetchImpl(`${this.baseUrl}${path}`, {
      method: 'POST',
      headers: this.headers(accessToken, workspaceId),
      body: JSON.stringify(body),
    });
    return this.parse<T>(response);
  }

  private async get<T>(path: string, accessToken: string, workspaceId?: string): Promise<T> {
    const response = await this.fetchImpl(`${this.baseUrl}${path}`, {
      method: 'GET',
      headers: this.headers(accessToken, workspaceId),
    });
    return this.parse<T>(response);
  }

  private headers(accessToken?: string, workspaceId?: string): Record<string, string> {
    const headers: Record<string, string> = { 'content-type': 'application/json' };
    if (accessToken) {
      headers['authorization'] = `Bearer ${accessToken}`;
    }
    if (workspaceId) {
      headers['x-workspace-id'] = workspaceId;
    }
    return headers;
  }

  private async parse<T>(response: Response): Promise<T> {
    if (!response.ok) {
      let message = `request failed with status ${response.status}`;
      try {
        const body = (await response.json()) as { error?: { message?: string } };
        if (body?.error?.message) {
          message = body.error.message;
        }
      } catch {
        // response had no JSON body
      }
      throw new AccountApiError(message, response.status);
    }
    if (response.status === 204) {
      return undefined as T;
    }
    return (await response.json()) as T;
  }
}
