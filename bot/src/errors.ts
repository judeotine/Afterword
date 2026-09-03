/**
 * Error types shared across the bot service.
 *
 * They exist so the HTTP layer can map a failure to a status code and the
 * worker can put a stable, human-readable reason on the job record.
 */

/** A platform or integration that is recognised but deliberately not built yet. */
export class NotImplementedError extends Error {
  readonly feature: string;

  constructor(feature: string, detail?: string) {
    super(
      detail ?? `${feature} is recognised but not yet supported by the Afterword meeting bot`,
    );
    this.name = 'NotImplementedError';
    this.feature = feature;
  }
}

/** The bot waited in the meeting lobby but was never let in. */
export class AdmissionTimeoutError extends Error {
  readonly waitedMs: number;

  constructor(waitedMs: number) {
    super(`Nobody admitted the notetaker within ${Math.round(waitedMs / 1000)}s`);
    this.name = 'AdmissionTimeoutError';
    this.waitedMs = waitedMs;
  }
}

/** The requested meeting URL does not belong to any platform the bot knows. */
export class UnsupportedPlatformError extends Error {
  readonly meetingUrl: string;
  readonly supported: readonly string[];

  constructor(meetingUrl: string, supported: readonly string[]) {
    super(
      `Unsupported meeting URL: ${meetingUrl}. Supported platforms: ${supported.join(', ')}`,
    );
    this.name = 'UnsupportedPlatformError';
    this.meetingUrl = meetingUrl;
    this.supported = supported;
  }
}
