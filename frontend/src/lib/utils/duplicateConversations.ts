/** The warning for one Claude conversation open in several places at once. */

export type DuplicateConversation = {
  conversationId: string;
  places: string[];
  tabs?: { sessionId: string; windowIdx: number; place: number }[];
};

export function describeDuplicateConversations(
  found: DuplicateConversation[],
  t: (key: string, params?: Record<string, string>) => string,
): string {
  return found.map((duplicate) => t('conversations.duplicate', {
    id: duplicate.conversationId.slice(0, 8),
    places: duplicate.places.map((place) => place || t('conversations.outsideApp')).join(', '),
  })).join('\n');
}
