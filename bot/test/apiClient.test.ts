import { describe, expect, it, vi } from 'vitest';

import { ApiClient, WorkerBotJob } from '../src/apiClient.js';
import { PollLoop } from '../src/pollLoop.js';

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === null ? null : JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}

describe('ApiClient', () => {
  const job: WorkerBotJob = {
    id: 'job-1',
    workspace_id: 'ws-1',
    meeting_url: 'https://meet.google.com/abc-defg-hij',
    platform: 'meet',
    scheduled_at: '2026-01-01T00:00:00Z',
    status: 'claimed',
    estimated_minutes: 60,
    minutes_used: 0,
  };

  it('returns null when no job is available', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    const client = new ApiClient({ baseUrl: 'http://api', workerToken: 't', workerId: 'w1', fetchImpl });
    expect(await client.claimNext()).toBeNull();
  });

  it('sends the worker token when claiming', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(jsonResponse(200, job));
    const client = new ApiClient({ baseUrl: 'http://api/', workerToken: 'secret', workerId: 'w1', fetchImpl });
    const claimed = await client.claimNext();
    expect(claimed?.id).toBe('job-1');
    const [url, init] = fetchImpl.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('http://api/v1/worker/bot-jobs/claim');
    expect((init.headers as Record<string, string>)['x-afterword-worker-token']).toBe('secret');
  });

  it('reports status to the job specific path', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(jsonResponse(200, { ...job, status: 'recording' }));
    const client = new ApiClient({ baseUrl: 'http://api', workerToken: 't', workerId: 'w1', fetchImpl });
    const updated = await client.reportStatus('job-1', { status: 'recording', consent_announced: true });
    expect(updated.status).toBe('recording');
    const [url, init] = fetchImpl.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('http://api/v1/worker/bot-jobs/job-1/status');
    expect(JSON.parse(init.body as string)).toMatchObject({ worker_id: 'w1', status: 'recording', consent_announced: true });
  });
});

describe('PollLoop', () => {
  it('invokes the job hook when a job is claimed', async () => {
    const job: WorkerBotJob = {
      id: 'job-2',
      workspace_id: 'ws-1',
      meeting_url: 'https://meet.google.com/x',
      platform: 'meet',
      scheduled_at: '2026-01-01T00:00:00Z',
      status: 'claimed',
      estimated_minutes: 60,
      minutes_used: 0,
    };
    const fetchImpl = vi.fn().mockResolvedValue(jsonResponse(200, job));
    const client = new ApiClient({ baseUrl: 'http://api', workerToken: 't', workerId: 'w1', fetchImpl });
    const loop = new PollLoop({ client, intervalMs: 1, sleep: async () => {} });
    const seen: string[] = [];
    const worked = await loop.runOnce({ onJob: async (j: WorkerBotJob) => { seen.push(j.id); } });
    expect(worked).toBe(true);
    expect(seen).toEqual(['job-2']);
  });

  it('reports no work when the claim is empty', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    const client = new ApiClient({ baseUrl: 'http://api', workerToken: 't', workerId: 'w1', fetchImpl });
    const loop = new PollLoop({ client, intervalMs: 1, sleep: async () => {} });
    const worked = await loop.runOnce({ onJob: async () => { throw new Error('should not run'); } });
    expect(worked).toBe(false);
  });
});
