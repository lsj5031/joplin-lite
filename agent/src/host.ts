/**
 * The only bridge to the Go host. The host object is installed before the
 * module loads; every method returns JSON text so the boundary stays a
 * versioned data contract rather than shared object graphs.
 */
export const API_VERSION = 1;

export type Scope =
  | { kind: 'note'; noteId: string }
  | { kind: 'notebook'; notebookId: string; includeDescendants: boolean }
  | { kind: 'vault' };

export interface Host {
  apiVersion: number;
  run: { id: string; conversationId: string; scope: Scope; limits: Limits };
  notesSearch(input: NoteSearchInput): Promise<string>;
  notesRead(input: NoteReadInput): Promise<string>;
  notesListNotebooks(): Promise<string>;
  notesListTags(): Promise<string>;
  dbCall(input: DbCallInput): Promise<string>;
  emit(event: AgentEvent): void;
  randomHex(bytes: number): string;
  randomUUID(): string;
  log(message: string): void;
}

export interface Limits {
  modelTurns: number;
  noteToolCalls: number;
  evidenceBytes: number;
  readBytes: number;
  searchLimit: number;
}

export interface NoteSearchInput {
  query: string;
  limit?: number;
  tagId?: string;
  cursor?: string;
}

export interface NoteReadInput {
  noteId: string;
  expectedVersion?: string;
  startByte?: number;
  maxBytes?: number;
}

export interface DbCallInput {
  op: 'exec' | 'run' | 'get' | 'all' | 'begin' | 'commit' | 'rollback';
  sql?: string;
  params?: unknown[];
  tx?: string;
}

export interface AgentEvent {
  type: string;
  [key: string]: unknown;
}

let cached: Host | undefined;

export function host(): Host {
  if (cached) return cached;
  const value = (globalThis as any).joplinHost as Host | undefined;
  if (!value || value.apiVersion !== API_VERSION) {
    throw new Error('Joplin Ask host is missing or has an unsupported API version.');
  }
  cached = value;
  return value;
}

export function emit(event: AgentEvent): void {
  try {
    host().emit(event);
  } catch {
    /* The host owns diagnostics; an event must not break the run. */
  }
}

export function log(message: string): void {
  try {
    host().log(String(message).slice(0, 2000));
  } catch {
    /* Logging is best effort. */
  }
}

export function installCrypto(): void {
  const g = globalThis as any;
  if (!g.crypto) g.crypto = {};
  if (typeof g.crypto.getRandomValues !== 'function') {
    g.crypto.getRandomValues = (array: any) => {
      const hex = host().randomHex(array.length);
      for (let i = 0; i < array.length; i++) array[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16);
      return array;
    };
  }
  if (typeof g.crypto.randomUUID !== 'function') {
    g.crypto.randomUUID = () => host().randomUUID();
  }
}
