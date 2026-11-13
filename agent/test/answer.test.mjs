import assert from 'node:assert/strict';
import { test } from 'node:test';
import { lastAnswer } from '../src/answer.ts';
import { openNodeSqliteStorage } from '@earendil-works/pi-durable/storage/sqlite/node';
import { BACKGROUND_CONTEXT } from '@earendil-works/chord/context';

function transcript(entries) {
  return {
    async entries(query, limit, cursor) {
      assert.equal(query.order, 'descending');
      const visible = entries.filter((entry) => !query.minEntryId || entry.id >= query.minEntryId).toReversed();
      const offset = cursor?.offset ?? 0;
      return { items: visible.slice(offset, offset + limit), next: offset + limit < visible.length ? { offset: offset + limit } : undefined };
    },
  };
}
const answer = (id, text) => ({ id, model: [{ role: 'assistant', content: [{ type: 'text', text }] }] });

test('answers come from the latest submission beyond 256 historical entries', async () => {
  const entries = Array.from({ length: 300 }, (_, i) => answer(String(i).padStart(4, '0'), 'old answer [E1]'));
  const boundary = entries.at(-1).id;
  entries.push(answer('0300', 'new answer [E7]'));
  assert.equal(await lastAnswer(transcript(entries), boundary), 'new answer [E7]');
});

test('follows continuation through bookkeeping and never falls back to an earlier submission', async () => {
  const entries = [answer('0000', 'old [E1]'), answer('0001', 'current [E2]')];
  for (let i = 2; i < 300; i++) entries.push({ id: String(i).padStart(4, '0'), kind: 'bookkeeping' });
  assert.equal(await lastAnswer(transcript(entries), '0000'), 'current [E2]');
  assert.equal(await lastAnswer(transcript(entries), '0001'), '');
});

test('installed Pi SQLite API honors newest order, inclusive boundary, and continuation', async () => {
  const storage = await openNodeSqliteStorage(':memory:', { prefix: 'pi' });
  try {
    const conversationId = await storage.mintId();
    await storage.commit([{ type: 'conversation', value: { id: conversationId } }], BACKGROUND_CONTEXT);
    const append = async (model) => {
      const id = await storage.mintId();
      await storage.commit([{ type: 'entry', value: { id, conversationId, kind: 'message', model } }], BACKGROUND_CONTEXT);
      return id;
    };
    let boundary;
    for (let i = 0; i < 300; i++) boundary = await append(answer('', 'old historical answer [E1]').model);
    const root = {
      entries(query, limit, cursor, context) {
        return storage.scanEntries({ ...query, conversationId }, limit, cursor, context);
      },
    };
    assert.equal(await lastAnswer(root, boundary), '');
    await append(answer('', 'new submission answer [E7]').model);
    assert.equal(await lastAnswer(root, boundary), 'new submission answer [E7]');
    for (let i = 0; i < 260; i++) await append(undefined);
    assert.equal(await lastAnswer(root, boundary), 'new submission answer [E7]');
  } finally {
    await storage.close(BACKGROUND_CONTEXT);
  }
});
