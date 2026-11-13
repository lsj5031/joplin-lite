import { BACKGROUND_CONTEXT } from '@earendil-works/chord/context';
import type { Conversation, EntryId } from '@earendil-works/pi-durable';

// The boundary is captured immediately before the only admitted submission.
// Scan newest first and follow Pi's opaque continuation, never an old answer.
export async function lastAnswer(root: Conversation, boundary?: EntryId): Promise<string> {
  let cursor;
  do {
    const page = await root.entries({ order: 'descending', minEntryId: boundary }, 256, cursor, BACKGROUND_CONTEXT);
    for (const entry of page.items) {
      if (entry.id === boundary) return '';
      if (!Array.isArray(entry.model)) continue;
      for (let i = entry.model.length - 1; i >= 0; i--) {
        const message = entry.model[i];
        if (message?.role !== 'assistant' || !Array.isArray(message.content)) continue;
        const text = message.content.filter((block) => block.type === 'text').map((block) => block.text).join('');
        if (text.trim()) return text;
      }
    }
    cursor = page.next;
  } while (cursor);
  return '';
}
