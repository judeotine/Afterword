export interface WorkerBotJob {
  id: string;
  workspace_id: string;
  meeting_url: string;
  platform: string;
  bot_name?: string;
  scheduled_at: string;
  status: string;
  estimated_minutes: number;
  minutes_used: number;
}

export interface WorkerStatusUpdate {
  status?: 'joining' | 'recording' | 'completed' | 'failed';
  minutes_used?: number;
  consent_announced?: boolean;
  error?: string;
}

export interface ApiClientOptions {
  baseUrl: string;
  workerToken: string;
  workerId: string;
  fetchImpl?: typeof fetch;
}

export class ApiClient {
  private readonly baseUrl: string;
  private readonly workerToken: string;
  private readonly workerId: string;
  private readonly fetchImpl: typeof fetch;

  constructor(options: ApiClientOptions) {
    this.baseUrl = options.baseUrl.replace(/\/+$/, '');
    this.workerToken = options.workerToken;
    this.workerId = options.workerId;
    this.fetchImpl = options.fetchImpl ?? fetch;
  }

  async claimNext(): Promise<WorkerBotJob | null> {
    const response = await this.fetchImpl(`${this.baseUrl}/v1/worker/bot-jobs/claim`, {
      method: 'POST',
      headers: this.headers(),
      body: JSON.stringify({ worker_id: this.workerId }),
    });
    if (response.status === 204) {
      return null;
    }
    if (!response.ok) {
      throw new Error(`claim failed: ${response.status}`);
    }
    return (await response.json()) as WorkerBotJob;
  }

  async reportStatus(jobId: string, update: WorkerStatusUpdate): Promise<WorkerBotJob> {
    const response = await this.fetchImpl(`${this.baseUrl}/v1/worker/bot-jobs/${jobId}/status`, {
      method: 'POST',
      headers: this.headers(),
      body: JSON.stringify({ worker_id: this.workerId, ...update }),
    });
    if (!response.ok) {
      throw new Error(`status update failed: ${response.status}`);
    }
    return (await response.json()) as WorkerBotJob;
  }

  private headers(): Record<string, string> {
    return {
      'content-type': 'application/json',
      'x-afterword-worker-token': this.workerToken,
    };
  }
}
