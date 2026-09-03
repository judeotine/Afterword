/** Environment configuration for the bot service, validated with zod. */
import { z } from 'zod';

const booleanish = z
  .enum(['true', 'false', '1', '0', 'yes', 'no'])
  .transform((value) => value === 'true' || value === '1' || value === 'yes');

export const configSchema = z.object({
  /** HTTP port for the job API. */
  PORT: z.coerce.number().int().positive().default(8787),
  /** Display name the bot uses when it joins a meeting. */
  BOT_NAME: z.string().min(1).default('Afterword Notetaker'),
  /** Where wav recordings and transcript directories are written. */
  RECORDINGS_DIR: z.string().min(1).default('./recordings'),
  /** Hard stop for a single meeting, in minutes. */
  MAX_MEETING_MINUTES: z.coerce.number().int().positive().default(180),
  /** Leave after being the only participant for this long. */
  ALONE_TIMEOUT_SECONDS: z.coerce.number().int().positive().default(120),
  /** The Rust transcription CLI shared with the desktop app. */
  TRANSCRIBE_BIN: z.string().min(1).default('afterword-transcribe'),
  TRANSCRIBE_ENGINE: z.enum(['whisper', 'parakeet']).default('whisper'),
  TRANSCRIBE_MODEL: z.string().min(1).default('base'),
  MODELS_DIR: z.string().min(1).default('./models'),
  /**
   * Link to the privacy policy the consent notice reads out. Required, with no
   * default: every participant is pointed at this URL, so a deployment has to
   * name a policy that actually describes it.
   */
  PRIVACY_URL: z.string().url(),
  /** Run Chromium headless. Set to false to watch the bot locally. */
  HEADLESS: booleanish.default('true'),
  /** Name of the PulseAudio null sink the browser plays into. */
  PULSE_SINK_NAME: z.string().min(1).default('afterword_sink'),
});

export type BotConfig = z.infer<typeof configSchema>;

/** Parse configuration from an environment-like record (defaults to process.env). */
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

/** Configuration for this process. */
export const config: BotConfig = loadConfig();
