import type { SyncState } from '~/lib/mock-data'
import { SyncLine } from './SyncLine'

const STATE_COPY: Record<SyncState, string> = {
  synced: 'All changes synced',
  syncing: 'Syncing — sending changes to the target',
  conflict: '1 conflict needs review',
  error: 'Sync failed — retry when the target is reachable',
  locked: 'Target locked by another client — sync refused',
}

/**
 * Measured footprint — 2026-10-17, windows/amd64, Go 1.26.4,
 * modernc.org/sqlite v1.60.1, `go build -trimpath -ldflags="-s -w"`:
 * default binary 16,351,232 bytes (16.4 MB), startup 74–133 ms over 3 runs.
 * The S3 target is behind `-tags s3` and adds ~8.2 MB (aws-sdk-go-v2).
 * These are real measurements, not placeholders — re-measure at M5 packaging.
 */
const FOOTPRINT = {
  binary: '16.4 MB',
  startup: '74–133 ms',
  measuredOn: '2026-10-17',
}

type Props = {
  state: SyncState
  lastSync: number
  noteCount: number
  notebookCount: number
  hasMore?: boolean
  source: 'mock' | 'loading' | 'live'
  error?: string | null
}

export function StatusStrip({ state, lastSync, noteCount, notebookCount, hasMore, source, error }: Props) {
  return (
    <footer className="flex flex-col-reverse items-stretch gap-1.5 border-t border-navy-line bg-navy px-4 py-2 text-paper md:flex-row md:items-center md:gap-8 md:px-6">
      <div className="min-w-0 flex-1 overflow-x-auto">
        <SyncLine state={state} />
      </div>
      <div className="flex shrink-0 flex-row flex-wrap items-baseline gap-x-3 gap-y-0.5 md:flex-col md:items-end md:gap-0.5 md:text-right">
        <span
          className="mono text-[11px] font-medium"
          style={{ color: state === 'synced' ? 'var(--color-moss-bright)' : state === 'conflict' ? 'var(--color-signal-bright)' : 'var(--color-route-soft)' }}
        >
          {STATE_COPY[state]}
        </span>
        <span className="mono text-[11px] text-navy-text">
          Last sync {lastSync ? new Date(lastSync).toLocaleTimeString('en-GB', { hour12: false }) : '—'} · {noteCount}{hasMore ? '+' : ''} notes · {notebookCount} notebooks
        </span>
        <span className="mono text-[10px] text-navy-text/70">
          Binary {FOOTPRINT.binary} · start {FOOTPRINT.startup} · measured {FOOTPRINT.measuredOn}
        </span>
        <span className="mono text-[10px] text-navy-text/70">
          {source === 'live'
            ? 'Live · your local Joplin database'
            : source === 'loading'
              ? 'Connecting to the local database…'
              : 'Synthetic prototype data'}
        </span>
        {error && (
          <span className="mono max-w-[70ch] truncate text-[10px] text-signal-bright" title={error}>
            {error}
          </span>
        )}
      </div>
    </footer>
  )
}
