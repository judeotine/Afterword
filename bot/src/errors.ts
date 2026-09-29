
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

export class AdmissionTimeoutError extends Error {
  readonly waitedMs: number;

  constructor(waitedMs: number) {
    super(`Nobody admitted the notetaker within ${Math.round(waitedMs / 1000)}s`);
    this.name = 'AdmissionTimeoutError';
    this.waitedMs = waitedMs;
  }
}

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
