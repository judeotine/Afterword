import { ApiClient, WorkerBotJob } from './apiClient.js';

export interface PollLoopHooks {
  onJob: (job: WorkerBotJob) => Promise<void>;
  onError?: (error: unknown) => void;
}

export interface PollLoopOptions {
  client: ApiClient;
  intervalMs: number;
  sleep?: (ms: number) => Promise<void>;
  now?: () => number;
}

const defaultSleep = (ms: number): Promise<void> => new Promise((resolve) => setTimeout(resolve, ms));

export class PollLoop {
  private readonly client: ApiClient;
  private readonly intervalMs: number;
  private readonly sleep: (ms: number) => Promise<void>;
  private running = false;

  constructor(options: PollLoopOptions) {
    this.client = options.client;
    this.intervalMs = options.intervalMs;
    this.sleep = options.sleep ?? defaultSleep;
  }

  stop(): void {
    this.running = false;
  }

  async runOnce(hooks: PollLoopHooks): Promise<boolean> {
    const job = await this.client.claimNext();
    if (!job) {
      return false;
    }
    await hooks.onJob(job);
    return true;
  }

  async run(hooks: PollLoopHooks, shouldContinue: () => boolean = () => true): Promise<void> {
    this.running = true;
    while (this.running && shouldContinue()) {
      try {
        const worked = await this.runOnce(hooks);
        if (!worked) {
          await this.sleep(this.intervalMs);
        }
      } catch (error) {
        if (hooks.onError) {
          hooks.onError(error);
        }
        await this.sleep(this.intervalMs);
      }
    }
  }
}
