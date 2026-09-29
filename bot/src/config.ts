import { z } from 'zod';

const booleanish = z
  .enum(['true', 'false', '1', '0', 'yes', 'no'])
  .transform((value) => value === 'true' || value === '1' || value === 'yes');

export const configSchema = z.object({
  PORT: z.coerce.number().int().positive().default(8787),
  BOT_NAME: z.string().min(1).default('Afterword Notetaker'),
  RECORDINGS_DIR: z.string().min(1).default('./recordings'),
  MAX_MEETING_MINUTES: z.coerce.number().int().positive().default(180),
  ALONE_TIMEOUT_SECONDS: z.coerce.number().int().positive().default(120),
  TRANSCRIBE_BIN: z.string().min(1).default('afterword-transcribe'),
  TRANSCRIBE_ENGINE: z.enum(['whisper', 'parakeet']).default('whisper'),
  TRANSCRIBE_MODEL: z.string().min(1).default('base'),
  MODELS_DIR: z.string().min(1).default('./models'),
  PRIVACY_URL: z.string().url(),
  HEADLESS: booleanish.default('true'),
  PULSE_SINK_NAME: z.string().min(1).default('afterword_sink'),
});

export type BotConfig = z.infer<typeof configSchema>;

export function loadConfig(env: NodeJS.ProcessEnv = process.env): BotConfig {
  const result = configSchema.safeParse(env);
  if (!result.success) {
    const issues = result.error.issues
      .map((issue) => `${issue.path.join('.')}: ${issue.message}`)
      .join('; ');
    throw new Error(`Invalid bot configuration: ${issues}`);
  }
  return result.data;
}

export const config: BotConfig = loadConfig();
