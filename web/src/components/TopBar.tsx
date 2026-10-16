import * as React from 'react'
import type { SyncState, VaultState } from '~/lib/mock-data'
import { IconGear, IconLock, IconPlus, IconSearch, IconSync, IconUnlock, Mark } from './icons'

type Props = {
  query: string
  onQuery: (q: string) => void
  syncState: SyncState
  onSync: () => void
  vault: VaultState
  onToggleVault: () => void
  onNewNote: () => void
  onPreviewState: (s: SyncState) => void
  searchRef: React.RefObject<HTMLInputElement | null>
}

export function TopBar({ query, onQuery, syncState, onSync, vault, onToggleVault, onNewNote, onPreviewState, searchRef }: Props) {
  const [settingsOpen, setSettingsOpen] = React.useState(false)

  return (
    <header className="relative z-20 flex items-center gap-2 border-b border-navy-line bg-navy px-3 py-2 text-paper md:gap-3 md:px-4">
      <div className="flex shrink-0 items-center gap-2.5">
        <Mark className="text-route-soft" />
        <span className="hidden text-[15px] font-semibold tracking-[-0.01em] sm:inline">joplin-lite</span>
      </div>

      <button
        type="button"
        onClick={onToggleVault}
        className={`flex shrink-0 items-center gap-1.5 rounded-[6px] border px-2.5 py-1 text-[12px] font-medium transition-colors duration-100 ${
          vault === 'unlocked'
            ? 'border-moss-bright/50 text-moss-bright hover:bg-moss-bright/10'
            : 'border-amber-bright/50 text-amber-bright hover:bg-amber-bright/10'
        }`}
        aria-label={vault === 'unlocked' ? 'Vault unlocked — click to lock' : 'Vault locked — click to unlock'}
      >
        {vault === 'unlocked' ? <IconUnlock width={14} height={14} /> : <IconLock width={14} height={14} />}
        <span className="hidden md:inline">{vault === 'unlocked' ? 'Vault unlocked' : 'Vault locked'}</span>
      </button>

      <div className="relative mx-auto w-full max-w-[420px]">
        <IconSearch width={15} height={15} className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-navy-text" />
        <input
          ref={searchRef}
          type="search"
          value={query}
          onChange={(e) => onQuery(e.target.value)}
          placeholder="Search notes, notebooks, tags…"
          aria-label="Search notes"
          className="h-8 w-full rounded-[6px] border border-navy-line bg-navy-2 pr-14 pl-8 text-[13px] text-paper placeholder:text-navy-text focus:border-route focus:outline-none"
        />
        <kbd className="mono pointer-events-none absolute top-1/2 right-2 hidden -translate-y-1/2 rounded-[4px] border border-navy-line px-1.5 py-0.5 text-[10px] text-navy-text md:block">
          ⌘K
        </kbd>
      </div>

      <div className="flex shrink-0 items-center gap-1.5">
        <button
          type="button"
          onClick={onNewNote}
          className="flex items-center gap-1.5 rounded-[6px] bg-route px-2.5 py-1.5 text-[12.5px] font-semibold text-paper transition-opacity duration-100 hover:opacity-90"
        >
          <IconPlus width={14} height={14} />
          <span className="hidden sm:inline">New note</span>
        </button>
        <button
          type="button"
          onClick={onSync}
          disabled={syncState === 'syncing'}
          className="flex items-center gap-1.5 rounded-[6px] border border-navy-line px-2.5 py-1.5 text-[12.5px] font-medium text-paper transition-colors duration-100 hover:bg-navy-line/60 disabled:opacity-50"
        >
          <IconSync width={14} height={14} className={syncState === 'syncing' ? 'animate-spin' : undefined} />
          <span className="hidden sm:inline">{syncState === 'syncing' ? 'Syncing…' : 'Sync now'}</span>
        </button>
        <button
          type="button"
          onClick={() => setSettingsOpen((v) => !v)}
          aria-expanded={settingsOpen}
          aria-label="Settings"
          className={`rounded-[6px] border p-1.5 transition-colors duration-100 ${
            settingsOpen ? 'border-route-soft bg-navy-line' : 'border-navy-line hover:bg-navy-line/60'
          }`}
        >
          <IconGear width={15} height={15} />
        </button>
      </div>

      {settingsOpen && (
        <div className="absolute top-[calc(100%+6px)] right-4 w-[300px] rounded-[8px] bg-paper-3 p-4 text-ink shadow-[0_6px_18px_rgba(15,29,45,0.22)]">
          <h2 className="label pb-2 text-ink-soft">Settings</h2>
          <dl className="space-y-2.5 text-[12.5px]">
            <div className="flex items-baseline justify-between gap-3">
              <dt className="text-ink-soft">Sync target</dt>
              <dd className="mono text-right text-ink">Dropbox · target 7 · App folder</dd>
            </div>
            <div className="flex items-baseline justify-between gap-3">
              <dt className="text-ink-soft">Master password</dt>
              <dd className="mono text-ink">set · never leaves this device</dd>
            </div>
            <div className="flex items-baseline justify-between gap-3">
              <dt className="text-ink-soft">Clipper</dt>
              <dd className="mono text-right text-ink">41184 · token f3c1…9ab</dd>
            </div>
          </dl>
          <p className="mt-3 border-t border-line pt-2.5 text-[11.5px] text-ink-soft">
            The clipper server cannot run beside Joplin desktop: if port 41184 is taken, joplin-lite
            reports it instead of starting.
          </p>
          <h2 className="label mt-3 border-t border-line pt-2.5 text-ink-soft">Prototype state preview</h2>
          <div className="mt-1.5 flex flex-wrap gap-1.5">
            {(
              [
                ['synced', 'Synced'],
                ['syncing', 'Syncing'],
                ['conflict', 'Conflict'],
                ['locked', 'Target locked'],
                ['error', 'Sync error'],
              ] as const
            ).map(([state, label]) => (
              <button
                key={state}
                type="button"
                onClick={() => onPreviewState(state)}
                aria-pressed={syncState === state}
                className={`rounded-[6px] border px-2 py-0.5 text-[11px] font-medium transition-colors duration-100 ${
                  syncState === state
                    ? 'border-violet bg-violet-soft text-ink'
                    : 'border-line-strong bg-paper text-ink hover:bg-paper-2'
                }`}
              >
                {label}
              </button>
            ))}
          </div>
          <p className="mono mt-2 text-[10px] text-ink-soft">Prototype — values are synthetic.</p>
        </div>
      )}
    </header>
  )
}
