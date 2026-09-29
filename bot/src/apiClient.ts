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

export interface RecordingRequest {
  title: string;
  duration_s: number;
  audio_extension: string;
  size_bytes: number;
}

export interface UploadTargets {
  audio_url: string;
  transcript_url: string;
  expires_at: string;
}

export interface RecordingResult {
  meeting: { id: string; workspace_id: string; title: string; status: string };
  upload: UploadTargets;
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

  async createRecording(jobId: string, recording: RecordingRequest): Promise<RecordingResult> {
    const response = await this.fetchImpl(`${this.baseUrl}/v1/worker/bot-jobs/${jobId}/recording`, {
      method: 'POST',
      headers: this.headers(),
      body: JSON.stringify({ worker_id: this.workerId, ...recording }),
    });
    if (!response.ok) {
      throw new Error(`recording failed: ${response.status}`);
    }
    return (await response.json()) as RecordingResult;
  }

  private headers(): Record<string, string> {
    return {
      'content-type': 'application/json',
      'x-afterword-worker-token': this.workerToken,
    };
  }
}
