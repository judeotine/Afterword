/**
 * Consent announcement.
 *
 * Afterword's hard rule: every participant is told the meeting is being
 * recorded *before* the recorder starts. The worker sends this message into the
 * meeting chat and only then starts capturing audio.
 */

export interface ConsentMessageInput {
  /** Display name the bot joined under. */
  botName: string;
  /** Display name of the user the bot is attending for, when known. */
  onBehalfOf?: string | undefined;
  /** Link to the privacy policy participants can read. */
  privacyUrl: string;
}

/** Build the one-line consent notice announced in the meeting chat. */
export function buildConsentMessage({
  botName,
  onBehalfOf,
  privacyUrl,
}: ConsentMessageInput): string {
  const actor = onBehalfOf?.trim()
    ? `${botName} on behalf of ${onBehalfOf.trim()}`
    : botName;
  const audience = onBehalfOf?.trim() ? 'them' : 'the person who invited it';

  return [
    `This meeting is being recorded and transcribed by ${actor}.`,
    `Recording, transcript and summary are shared with ${audience}.`,
    `Privacy policy: ${privacyUrl}.`,
    'If you object, ask the host to remove the notetaker.',
  ].join(' ');
}
