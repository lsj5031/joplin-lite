/**
 * Pi Durable's portable SqliteDatabase over the Go-owned assistant.sqlite.
 *
 * All statements are serialized through one promise queue. A transaction holds
 * that queue until its callback settles, so unrelated statements cannot enter
 * the transaction or deadlock behind it. Values cross the host boundary tagged
 * so bigint, bytes and null are never guessed from JSON text.
 */
import { host, type DbCallInput } from './host.ts';

type SqliteValue = null | number | bigint | string | Uint8Array;

interface Tagged {
  t: 'n' | 'i' | 'f' | 's' | 'b';
  v?: string | number;
}

function encodeValue(value: SqliteValue): Tagged {
  if (value === null || value === undefined) return { t: 'n' };
  if (typeof value === 'bigint') return { t: 'i', v: value.toString() };
  if (typeof value === 'number') {
    if (!Number.isFinite(value)) throw new Error('SQLite cannot store a non-finite number.');
    return Number.isInteger(value) ? { t: 'i', v: String(value) } : { t: 'f', v: value };
  }
  if (typeof value === 'string') return { t: 's', v: value };
  if (value instanceof Uint8Array) return { t: 'b', v: bytesToBase64(value) };
  throw new Error(`Unsupported SQLite value type: ${typeof value}`);
}

function decodeValue(value: Tagged | null): unknown {
  if (!value || value.t === 'n') return null;
  if (value.t === 'b') return base64ToBytes(String(value.v ?? ''));
  if (value.t === 'i') {
    const text = String(value.v ?? '0');
    const number = Number(text);
    return Number.isSafeInteger(number) ? number : BigInt(text);
  }
  if (value.t === 'f') return Number(value.v);
  return value.v;
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = '';
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  }
  return btoa(binary);
}

function base64ToBytes(text: string): Uint8Array {
  const binary = atob(text);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

async function call(input: DbCallInput): Promise<any> {
  const raw = await host().dbCall({ ...input, params: input.params?.map((value) => encodeValue(value as SqliteValue)) });
  const result = JSON.parse(raw);
  if (result && result.error) {
    const detail = result.error.message || result.error.code || JSON.stringify(result.error);
    throw new Error(String(detail));
  }
  return result;
}

class Executor {
  readonly tx: string | null;

  constructor(tx: string | null) {
    this.tx = tx;
  }

  async exec(sql: string): Promise<void> {
    await call({ op: 'exec', sql, tx: this.tx ?? undefined });
  }

  async run(sql: string, ...params: SqliteValue[]): Promise<void> {
    await call({ op: 'run', sql, params, tx: this.tx ?? undefined });
  }

  async get<T extends object>(sql: string, ...params: SqliteValue[]): Promise<T | undefined> {
    const result = await call({ op: 'get', sql, params, tx: this.tx ?? undefined });
    if (!result?.row) return undefined;
    return Object.fromEntries(Object.entries(result.row).map(([key, value]) => [key, decodeValue(value as Tagged)])) as T;
  }

  async all<T extends object>(sql: string, ...params: SqliteValue[]): Promise<T[]> {
    const result = await call({ op: 'all', sql, params, tx: this.tx ?? undefined });
    return (result?.rows ?? []).map((row: Record<string, Tagged>) =>
      Object.fromEntries(Object.entries(row).map(([key, value]) => [key, decodeValue(value)])),
    ) as T[];
  }
}

export class HostSqliteDatabase {
  #queue: Promise<unknown> = Promise.resolve();
  #closed = false;
  #tx: string | null = null;

  #enqueue<T>(work: () => Promise<T>): Promise<T> {
    if (this.#closed) return Promise.reject(new Error('The assistant database is closed.'));
    const run = this.#queue.then(work, work);
    this.#queue = run.then(
      () => undefined,
      () => undefined,
    );
    return run;
  }

  exec(sql: string): Promise<void> {
    return this.#enqueue(async () => {
      if (this.#tx) throw new Error('Use the active transaction handle.');
      await call({ op: 'exec', sql });
    });
  }

  run(sql: string, ...params: SqliteValue[]): Promise<void> {
    return this.#enqueue(async () => {
      if (this.#tx) throw new Error('Use the active transaction handle.');
      await call({ op: 'run', sql, params });
    });
  }

  get<T extends object>(sql: string, ...params: SqliteValue[]): Promise<T | undefined> {
    return this.#enqueue(async () => {
      if (this.#tx) throw new Error('Use the active transaction handle.');
      const result = await call({ op: 'get', sql, params });
      if (!result?.row) return undefined;
      return Object.fromEntries(Object.entries(result.row).map(([key, value]) => [key, decodeValue(value as Tagged)])) as T;
    });
  }

  all<T extends object>(sql: string, ...params: SqliteValue[]): Promise<T[]> {
    return this.#enqueue(async () => {
      if (this.#tx) throw new Error('Use the active transaction handle.');
      const result = await call({ op: 'all', sql, params });
      return (result?.rows ?? []).map((row: Record<string, Tagged>) =>
        Object.fromEntries(Object.entries(row).map(([key, value]) => [key, decodeValue(value)])),
      ) as T[];
    });
  }

  transaction<T>(callback: (tx: Executor) => Promise<T>): Promise<T> {
    return this.#enqueue(async () => {
      if (this.#tx) throw new Error('Nested transactions are not supported.');
      const begun = await call({ op: 'begin' });
      const tx = String(begun.tx);
      this.#tx = tx;
      let result: T;
      try {
        result = await callback(new Executor(tx));
      } catch (error) {
        try {
          await call({ op: 'rollback', tx });
        } catch (rollback) {
          throw new AggregateError([error, rollback], 'The transaction callback failed and rollback also failed.');
        } finally {
          this.#tx = null;
        }
        throw error;
      }
      try {
        await call({ op: 'commit', tx });
      } finally {
        this.#tx = null;
      }
      return result;
    });
  }

  close(): Promise<void> {
    return this.#enqueue(async () => {
      this.#closed = true;
      await call({ op: 'exec', sql: 'PRAGMA optimize' });
    });
  }
}
