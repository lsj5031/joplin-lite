import { BACKGROUND_CONTEXT, withAbortSignal } from '@earendil-works/chord/context';
import { watchEvents, type AgentEvent as PiEvent } from '@earendil-works/pi-durable';
import { Harness, createRegistry, defineExtension } from '@earendil-works/pi-durable';
import { SqliteStorage } from '@earendil-works/pi-durable/storage/sqlite';
import { host, emit, log, installCrypto, type AgentEvent } from './host.js';
import { createAskModels, PROVIDER_ID, type ModelConfig } from './models.js';
import { createNoteTools } from './tools.js';
import { ASK_INSTRUCTIONS } from './instructions.js';
import { HostSqliteDatabase } from './sqlite.js';
import { lastAnswer } from './answer.js';

interface OpenConfig {
  apiVersion: number;
  conversationId: string;
  model: ModelConfig;
  timeoutMs?: number;
}

interface RunState {
  requestId: string;
  text: string;
  tools: Map<string, { name: string; status: 'running' | 'done' | 'error' }>;
}

// Keep the event loop alive between user turns. The harness has no work while
// idle, and Loop.Run would otherwise return and release the runtime.
const keepAlive = setInterval(() => {}, 1 << 30);

// moejs intentionally installs no browser console. Pi's provider SDK writes
// diagnostics through it, so route those to the host log instead of faking a
// network or storage capability.
// The OpenAI SDK references these constructors in `instanceof` checks while
// building a request, even when the request is plain JSON. They are not
// implemented: attachment/upload paths are not exposed to this agent, and
// constructing one throws rather than pretending a file was uploaded.
for (const name of ['FormData', 'Blob', 'File'] as const) {
  if (!(globalThis as any)[name]) {
    (globalThis as any)[name] = class Unsupported {
      constructor() {
        throw new Error(`${name} is unavailable in the embedded Ask agent.`);
      }
    };
  }
}

if (!(globalThis as any).console) {
  const write = (...parts: unknown[]) => log(parts.map((part) => typeof part === 'string' ? part : JSON.stringify(part)).join(' '));
  (globalThis as any).console = { log: write, info: write, warn: write, error: write, debug: write };
}

let root: any;
let harness: any;
let storage: any;
let watcher: any;
let active: RunState | undefined;
let closed = false;

function activity(state: RunState, name: string, status: 'running' | 'done' | 'error', label?: string) {
  emit({
    type: 'activity.updated',
    requestId: state.requestId,
    activity: { tool: name, status, label: label ?? name, active: [...state.tools.values()].filter((t) => t.status === 'running').length },
  });
}

function project(event: PiEvent): void {
  const state = active;
  switch (event.type) {
    case 'run_start':
      if (state) {
        state.text = '';
        state.tools.clear();
      }
      emit({ type: 'activity.updated', requestId: state?.requestId, activity: { kind: 'thinking', label: 'Thinking', active: 1 } });
      break;
    case 'message_start':
      if (state && event.message?.role === 'assistant') state.text = '';
      break;
    case 'message_update':
      if (!state) break;
      for (const change of event.changes ?? []) {
        if (change.type === 'text_delta' && change.delta) {
          state.text += change.delta;
          emit({ type: 'answer.delta', requestId: state.requestId, text: change.delta });
        }
      }
      break;
    case 'tool_execution_start': {
      if (!state) break;
      const tool = { name: event.toolName, status: 'running' as const };
      state.tools.set(event.toolCallId, tool);
      activity(state, event.toolName, 'running', event.toolName === 'search_notes' ? 'Searching notes' : event.toolName === 'read_note' ? 'Reading a note' : event.toolName);
      break;
    }
    case 'tool_execution_end': {
      if (!state) break;
      const tool = state.tools.get(event.toolCallId);
      if (tool) tool.status = 'done';
      activity(state, event.toolName, 'done', event.toolName === 'search_notes' ? 'Searched notes' : event.toolName === 'read_note' ? 'Read a note' : event.toolName);
      break;
    }
    case 'run_end':
      if (state) emit({ type: 'answer.snapshot', requestId: state.requestId, text: state.text });
      emit({ type: 'activity.updated', requestId: state?.requestId, activity: { kind: 'idle', label: '', active: 0 } });
      break;
    case 'task_failed':
      emit({ type: 'diagnostic', message: event.message, taskId: event.taskId });
      break;
  }
}

function asError(error: unknown): { code: string; message: string } {
  const message = error instanceof Error ? error.message : String(error);
  const lower = message.toLowerCase();
  const code = lower.includes('abort') || lower.includes('cancel')
    ? 'cancelled'
    : lower.includes('401') || lower.includes('unauthorized') || lower.includes('auth')
      ? 'provider_auth'
      : lower.includes('429') || lower.includes('rate limit')
        ? 'provider_rate_limit'
        : lower.includes('database') || lower.includes('sqlite')
          ? 'storage_failed'
          : 'runtime_failed';
  return { code, message: message.slice(0, 1000) };
}

export async function open(config: OpenConfig): Promise<void> {
  try {
    if (closed) throw new Error('The agent is closed.');
    if (config?.apiVersion !== 1) throw new Error('Unsupported open config version.');
    installCrypto();
    const h = host();
    const db = new HostSqliteDatabase();
    storage = await SqliteStorage.open(db as any, { prefix: 'pi' });
    const models = createAskModels(config.model);
    const registry = createRegistry();
    registry.install(defineExtension({ name: 'joplin-notes', tools: createNoteTools() as any }));
    harness = await Harness.open(
      storage,
      {
        models,
        registry,
        settings: {
          retry: { enabled: false },
          stream: { timeoutMs: config.timeoutMs ?? 120000, maxRetries: 0 },
          progress: { partialIntervalMs: 60, outputIntervalMs: 160 },
        },
      },
      BACKGROUND_CONTEXT,
    );
    root = await harness.root(BACKGROUND_CONTEXT, {
      agent: {
        model: { provider: PROVIDER_ID, modelId: config.model.modelId },
        instructions: ASK_INSTRUCTIONS,
      },
    });
    await root.configure(
      {
        model: { provider: PROVIDER_ID, modelId: config.model.modelId },
        instructions: ASK_INSTRUCTIONS,
      },
      BACKGROUND_CONTEXT,
    );
    watcher = await watchEvents(harness, root.id, BACKGROUND_CONTEXT);
    watcher.start(async (events: readonly PiEvent[]) => {
      for (const event of events) {
        try {
          project(event);
        } catch (error) {
          log(`event projection failed: ${asError(error).message}`);
        }
      }
    });
    const agent = await root.agent(BACKGROUND_CONTEXT);
    h.log('agent runtime opened model=' + JSON.stringify(agent.model) + ' tools=' + agent.tools.map((tool: any) => tool.name).join(','));
    emit({ type: 'runtime.ready', conversationId: config.conversationId });
    // Let the host start the first submission only after open resolves.
    await Promise.resolve();
  } catch (error) {
    emit({ type: 'run.failed', ...asError(error), phase: 'open' });
    throw error;
  }
}

export async function submit(request: { requestId: string; text: string; abortSignal?: unknown }): Promise<void> {
  if (closed || !root) {
    emit({ type: 'run.failed', code: 'runtime_failed', message: 'The agent runtime is not open.' });
    return;
  }
  if (active) {
    emit({ type: 'run.failed', code: 'busy', message: 'Another Ask run is already active.' });
    return;
  }
  if (!request?.requestId || !request.text?.trim()) {
    emit({ type: 'run.failed', code: 'invalid_request', message: 'An Ask request needs text and a request ID.' });
    return;
  }
  active = { requestId: request.requestId, text: '', tools: new Map() };
  try {
    const previous = await root.entries({ order: 'descending' }, 1, undefined, BACKGROUND_CONTEXT);
    const boundary = previous.items[0]?.id;
    const submission = await root.submit({ type: 'input', content: request.text, requestId: request.requestId }, BACKGROUND_CONTEXT);
    const settled = await submission.wait(BACKGROUND_CONTEXT);
    host().log('submission settled ' + JSON.stringify(settled));
    if (settled.status !== 'done') {
      const detail = String(settled.detail || settled.reason || 'The model did not answer.');
      const lower = detail.toLowerCase();
      const code = lower.includes('401') || lower.includes('unauthorized')
        ? 'provider_auth'
        : lower.includes('429') || lower.includes('rate limit')
          ? 'provider_rate_limit'
          : lower.includes('fetch') || lower.includes('timeout') || lower.includes('connection')
            ? 'provider_unavailable'
            : 'runtime_failed';
      emit({ type: 'run.failed', code, message: detail.slice(0, 1000), requestId: request.requestId });
      return;
    }
    // Tool results and the final assistant message are committed before wait()
    // resolves; give the event watcher its queued batch before reading.
    await new Promise((resolve) => setTimeout(resolve, 0));
    const text = (await lastAnswer(root, boundary)) || active.text;
    emit({ type: 'answer.snapshot', requestId: request.requestId, text });
    emit({
      type: 'run.completed',
      text,
      status: settled.status,
      submissionId: String(settled.id),
      requestId: request.requestId,
    });
  } catch (error) {
    const failure = asError(error);
    if (failure.code === 'cancelled') emit({ type: 'run.cancelled', reason: failure.message, requestId: request.requestId });
    else emit({ type: 'run.failed', code: failure.code, message: failure.message, requestId: request.requestId });
  } finally {
    active = undefined;
  }
}

// Reset starts a new model context without discarding Pi's durable tables or
// the open runtime. Go calls this before admitting a run whose scope differs
// from the previous one.
export async function reset(): Promise<void> {
  if (!root) {
    emit({ type: 'conversation.reset', error: 'The agent runtime is not open.' });
    return;
  }
  try {
    await root.reset(undefined, BACKGROUND_CONTEXT);
    emit({ type: 'conversation.reset' });
  } catch (error) {
    const cause = (error as any)?.cause;
    log(`reset failed: ${String(cause?.message ?? cause ?? error)}`);
    emit({ type: 'conversation.reset', error: asError(error).message });
  }
}

export async function abort(reason = 'Stopped by the user.'): Promise<void> {
  if (!root) return;
  try {
    await root.abort(BACKGROUND_CONTEXT);
    emit({ type: 'run.cancelled', requestId: active?.requestId, reason });
  } catch (error) {
    emit({ type: 'run.failed', ...asError(error) });
  }
}

export async function snapshot(): Promise<string> {
  if (!root) return JSON.stringify({ messages: [] });
  try {
    const page = await root.entries({ order: 'ascending' }, 256, undefined, BACKGROUND_CONTEXT);
    const messages: any[] = [];
    for (const entry of page.items) {
      if (!Array.isArray(entry.model)) continue;
      for (const message of entry.model) {
        if (message?.role === 'user') {
          messages.push({ role: 'user', text: typeof message.content === 'string' ? message.content : (message.content ?? []).filter((b: any) => b?.type === 'text').map((b: any) => b.text).join('') });
        } else if (message?.role === 'assistant') {
          messages.push({ role: 'assistant', text: (message.content ?? []).filter((b: any) => b?.type === 'text').map((b: any) => b.text).join('') });
        }
      }
    }
    return JSON.stringify({ messages });
  } catch (error) {
    return JSON.stringify({ messages: [], error: asError(error).message });
  }
}

function bounded<T>(promise: Promise<T>, ms: number): Promise<T> {
  return Promise.race([
    promise,
    new Promise<T>((_, reject) => setTimeout(() => reject(new Error('cleanup timed out')), ms)),
  ]);
}

export async function close(): Promise<void> {
  if (closed) return;
  closed = true;
  clearInterval(keepAlive);
  try {
    if (watcher) await bounded(watcher.stop(), 1500).catch(() => undefined);
    if (root) await bounded(root.abort(BACKGROUND_CONTEXT), 1500).catch(() => undefined);
    if (harness) await bounded(harness.close(BACKGROUND_CONTEXT), 3000).catch(() => undefined);
  } catch (error) {
    host().log('close: ' + asError(error).message);
  } finally {
    emit({ type: 'runtime.closed' });
    root = undefined;
    harness = undefined;
    storage = undefined;
    watcher = undefined;
  }
}
