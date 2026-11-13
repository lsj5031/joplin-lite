import assert from 'node:assert/strict';
import { test } from 'node:test';

// The portable facade talks only to the versioned host object. This fixture
// records requests and returns tagged rows exactly like the Go bridge does.
const calls = [];
let nextTx = 1;
globalThis.joplinHost = {
  apiVersion: 1,
  async dbCall(input) {
    calls.push(input);
    if (input.op === 'begin') return JSON.stringify({ tx: `tx-${nextTx++}` });
    if (input.op === 'commit' || input.op === 'rollback') return JSON.stringify({});
    if (input.op === 'get') {
      if (input.sql.includes('missing')) return JSON.stringify({});
      return JSON.stringify({
        row: {
          i: { t: 'i', v: '9007199254740993' },
          f: { t: 'f', v: 1.5 },
          n: { t: 'n' },
          b: { t: 'b', v: 'AAEC' },
          s: { t: 's', v: 'hello' },
        },
      });
    }
    if (input.op === 'all') return JSON.stringify({ rows: [{ id: { t: 'i', v: '7' } }] });
    return JSON.stringify({ lastInsertId: 3, rowsAffected: 1 });
  },
};

const { HostSqliteDatabase } = await import('../src/sqlite.ts');

test('encodes bigint, bytes and numbers distinctly', async () => {
  calls.length = 0;
  const db = new HostSqliteDatabase();
  await db.run('INSERT INTO t VALUES (?, ?, ?, ?)', 42n, 2.5, new Uint8Array([0, 1, 2]), 'x');
  assert.deepEqual(calls[0].params, [
    { t: 'i', v: '42' },
    { t: 'f', v: 2.5 },
    { t: 'b', v: 'AAEC' },
    { t: 's', v: 'x' },
  ]);
});

test('decodes rows without losing bigint, bytes or null', async () => {
  const db = new HostSqliteDatabase();
  const row = await db.get('SELECT * FROM t');
  assert.equal(row.i, 9007199254740993n);
  assert.equal(row.f, 1.5);
  assert.equal(row.n, null);
  assert.deepEqual([...row.b], [0, 1, 2]);
  assert.equal(row.s, 'hello');
  assert.equal(await db.get('SELECT * FROM missing'), undefined);
});

test('commits a resolved transaction and rolls back a rejection', async () => {
  calls.length = 0;
  nextTx = 1;
  const db = new HostSqliteDatabase();
  const value = await db.transaction(async (tx) => {
    await tx.run('UPDATE t SET v = ?', 'ok');
    return 'done';
  });
  assert.equal(value, 'done');
  assert.deepEqual(calls.map((call) => call.op), ['begin', 'run', 'commit']);
  assert.equal(calls[1].tx, 'tx-1');
  assert.equal(calls[2].tx, 'tx-1');

  calls.length = 0;
  await assert.rejects(
    db.transaction(async (tx) => {
      await tx.run('UPDATE t SET v = ?', 'bad');
      throw new Error('callback failed');
    }),
    /callback failed/,
  );
  assert.deepEqual(calls.map((call) => call.op), ['begin', 'run', 'rollback']);
});

test('a transaction holds the queue until its callback settles', async () => {
  calls.length = 0;
  const db = new HostSqliteDatabase();
  let release;
  const gate = new Promise((resolve) => { release = resolve; });
  const transaction = db.transaction(async (tx) => {
    await gate;
    await tx.run('UPDATE t SET v = ?', 'inside');
  });
  const unrelated = db.run('UPDATE t SET v = ?', 'outside');
  await new Promise((resolve) => setTimeout(resolve, 10));
  assert.deepEqual(calls.map((call) => call.op), ['begin']);
  release();
  await Promise.all([transaction, unrelated]);
  const ops = calls.map((call) => call.op);
  assert.ok(ops.indexOf('commit') < ops.lastIndexOf('run'));
  const outside = calls.findLast((call) => call.op === 'run');
  assert.equal(outside.tx, undefined);
});

test('transaction executors cannot nest and closed databases reject work', async () => {
  const db = new HostSqliteDatabase();
  await db.transaction(async (tx) => {
    assert.equal(typeof tx.transaction, 'undefined');
  });
  await db.close();
  await assert.rejects(db.run('SELECT 1'), /closed/);
});
