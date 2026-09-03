/**
 * HTTP API for the meeting bot: create, inspect and cancel meeting jobs.
 *
 * Jobs live in memory only (see README limitations) and are executed by
 * scheduler.ts, which spawns one worker process per meeting.
 */
import { fileURLToPath } from 'node:url';
import Fastify, { type FastifyInstance } from 'fastify';
import { z } from 'zod';
import { config } from './config.js';
import { UnsupportedPlatformError } from './errors.js';
import { detectPlatform, RECOGNISED_PLATFORMS, SUPPORTED_PLATFORMS } from './platforms/types.js';
import { ChildProcessRunner, Scheduler, type JobRunner } from './scheduler.js';

const createJobSchema = z.object({
  meetingUrl: z.string().min(1),
  botName: z.string().min(1).optional(),
  startAt: z.string().datetime({ offset: true }).optional(),
  onBehalfOf: z.string().min(1).optional(),
  consentMessage: z.string().min(1).optional(),
});

const jobParamsSchema = z.object({ id: z.string().min(1) });

export interface ServerOptions {
  runner?: JobRunner;
  scheduler?: Scheduler;
}

/** Build the Fastify app. Injecting a runner keeps the API testable. */
export function buildServer(options: ServerOptions = {}): FastifyInstance {
  const scheduler =
    options.scheduler ?? new Scheduler({ runner: options.runner ?? new ChildProcessRunner() });
  const app = Fastify({ logger: { name: 'afterword-bot' } });

  app.get('/healthz', async () => ({
    status: 'ok',
    supportedPlatforms: SUPPORTED_PLATFORMS,
  }));

  app.post('/jobs', async (request, reply) => {
    const parsed = createJobSchema.safeParse(request.body);
    if (!parsed.success) {
      return reply.status(400).send({
        error: 'Invalid job request',
        issues: parsed.error.issues.map((issue) => ({
          path: issue.path.join('.'),
          message: issue.message,
        })),
      });
    }

    const platform = detectPlatform(parsed.data.meetingUrl);
    if (!platform) {
      return reply.status(400).send({
        error: 'Unrecognised meeting URL',
        supportedPlatforms: SUPPORTED_PLATFORMS,
        recognisedPlatforms: RECOGNISED_PLATFORMS,
      });
    }
    if (!SUPPORTED_PLATFORMS.includes(platform.name)) {
      return reply.status(501).send({
        error: `${platform.name} is recognised but not yet supported`,
        supportedPlatforms: SUPPORTED_PLATFORMS,
      });
    }

    try {
      const job = scheduler.create(parsed.data);
      return reply.status(201).send({ id: job.id, status: job.status });
    } catch (error) {
      if (error instanceof UnsupportedPlatformError) {
        return reply
          .status(400)
          .send({ error: error.message, supportedPlatforms: SUPPORTED_PLATFORMS });
      }
      throw error;
    }
  });

  app.get('/jobs/:id', async (request, reply) => {
    const params = jobParamsSchema.safeParse(request.params);
    if (!params.success) {
      return reply.status(400).send({ error: 'Invalid job id' });
    }
    const job = scheduler.get(params.data.id);
    if (!job) {
      return reply.status(404).send({ error: 'No such job' });
    }
    return reply.send(job);
  });

  app.delete('/jobs/:id', async (request, reply) => {
    const params = jobParamsSchema.safeParse(request.params);
    if (!params.success) {
      return reply.status(400).send({ error: 'Invalid job id' });
    }
    const existing = scheduler.get(params.data.id);
    if (!existing) {
      return reply.status(404).send({ error: 'No such job' });
    }
    if (!scheduler.cancel(params.data.id)) {
      return reply
        .status(409)
        .send({ error: `Job already finished with status ${existing.status}` });
    }
    return reply.status(202).send(scheduler.get(params.data.id));
  });

  return app;
}

export async function start(): Promise<FastifyInstance> {
  const app = buildServer();
  await app.listen({ port: config.PORT, host: '0.0.0.0' });
  return app;
}

const invokedDirectly =
  process.argv[1] !== undefined && fileURLToPath(import.meta.url) === process.argv[1];

if (invokedDirectly) {
  await start();
}
