import { Type } from 'typebox';
import { defineTool } from '@earendil-works/pi-durable';
import { host } from './host.js';

const text = (value: unknown) => ({ output: [{ type: 'text', text: JSON.stringify(value) }] });

async function call(method: 'notesSearch' | 'notesRead' | 'notesListNotebooks' | 'notesListTags', input?: unknown) {
  const raw = method === 'notesSearch' || method === 'notesRead'
    ? await host()[method](input as any)
    : await host()[method]();
  let value: any;
  try {
    value = JSON.parse(raw);
  } catch {
    throw new Error('The note host returned an unreadable response.');
  }
  if (value?.error) {
    const error = new Error(String(value.error.message || value.error.code || 'Note request failed.'));
    (error as any).code = value.error.code;
    throw error;
  }
  return value;
}

/**
 * Read-only note tools. Their scope belongs to the Go host; these argument
 * schemas deliberately contain no folder/vault selector, so the model cannot
 * widen what the user asked for.
 */
export function createNoteTools() {
  return [
    defineTool({
      name: 'search_notes',
      description:
        'Search the user-selected notes. Returns ranked note IDs, titles and short snippets only. Use this first, then read_note for evidence. Empty results mean the indexed text had no match; try different terms, synonyms, or a narrower phrase.',
      parameters: Type.Object({
        query: Type.String({ minLength: 1, maxLength: 400 }),
        limit: Type.Optional(Type.Integer({ minimum: 1, maximum: 20 })),
        tagId: Type.Optional(Type.String({ maxLength: 64 })),
        cursor: Type.Optional(Type.String({ maxLength: 200 })),
      }),
      execute: async (args) => text(await call('notesSearch', args)),
    }),
    defineTool({
      name: 'read_note',
      description:
        'Read a bounded UTF-8 passage from a note found by search_notes. Returns an evidence ID such as E3; cite that token in the answer. Pass nextByte to continue a long note. A changed or missing note is reported explicitly, never as empty evidence.',
      parameters: Type.Object({
        noteId: Type.String({ minLength: 1, maxLength: 64 }),
        expectedVersion: Type.Optional(Type.String({ maxLength: 128 })),
        startByte: Type.Optional(Type.Integer({ minimum: 0, maximum: 100000000 })),
        maxBytes: Type.Optional(Type.Integer({ minimum: 1, maximum: 12288 })),
      }),
      execute: async (args) => text(await call('notesRead', args)),
    }),
    defineTool({
      name: 'list_notebooks',
      description: 'List notebooks visible in the current scope. Use this only to orient yourself; it is not evidence.',
      parameters: Type.Object({}),
      execute: async () => text(await call('notesListNotebooks')),
    }),
    defineTool({
      name: 'list_tags',
      description: 'List tags available in the current scope. Use this only to refine a search; it is not evidence.',
      parameters: Type.Object({}),
      execute: async () => text(await call('notesListTags')),
    }),
  ];
}
