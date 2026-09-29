
export interface ConsentMessageInput {
  botName: string;
  onBehalfOf?: string | undefined;
  privacyUrl: string;
}

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
