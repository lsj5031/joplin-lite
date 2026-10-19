import * as React from 'react'
import type { VaultSource } from '~/lib/vault'
import { IconGear, IconPlus, IconSearch, IconSync, Mark } from './icons'

type Props = {
  query: string
  onQuery: (q: string) => void
  source: VaultSource
  canWrite: boolean
  reloading: boolean
  onReload: () => void
  onNewNote: () => void
  searchRef: React.RefObject<HTMLInputElement | null>
}

export function TopBar({ query, onQuery, source, canWrite, reloading, onReload, onNewNote, searchRef }: Props) {
  const settingsRef = React.useRef<HTMLDialogElement>(null)
  const [theme, setTheme] = React.useState<string | null>(null)
  React.useEffect(() => {
    try {
      const value = localStorage.getItem('joplin-lite:theme')
      setTheme(value === 'light' || value === 'dark' ? value : 'system')
    } catch { setTheme('system') }
  }, [])
  React.useEffect(() => {
    if (!theme) return
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const apply = () => { document.documentElement.dataset.theme = theme === 'system' ? media.matches ? 'dark' : 'light' : theme }
    apply()
    media.addEventListener('change', apply)
    return () => media.removeEventListener('change', apply)
  }, [theme])
  const chooseTheme = (value: string) => {
    try { localStorage.setItem('joplin-lite:theme', value) } catch { /* Still apply it for this session. */ }
    setTheme(value)
  }
  const demo = source === 'mock'
  const reloadLabel = source === 'error' ? 'Retry connection' : 'Reload local database'

  return (
    <header className="app-header flex shrink-0 flex-wrap items-center gap-3 border-b border-line px-4 py-3 text-ink md:flex-nowrap md:gap-6 md:px-6">
      <div className="flex shrink-0 items-center gap-3 md:w-[194px]">
        <Mark />
        <div className="brand-name">joplin-lite</div>
      </div>

      <div className="relative order-last w-full md:order-none md:mx-auto md:max-w-[420px] md:min-w-0">
        <IconSearch width={17} height={17} className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-ink-soft" />
        <input
          ref={searchRef}
          type="search"
          value={query}
          onChange={(e) => onQuery(e.target.value)}
          placeholder="Search all notes…"
          aria-label="Search notes"
          className="h-11 w-full rounded-lg border border-line bg-paper pr-8 pl-10 text-[16px] text-ink placeholder:text-ink-soft focus:border-route focus:outline-none md:pr-14 md:text-[13px]"
        />
        <kbd className="mono pointer-events-none absolute top-1/2 right-3 hidden -translate-y-1/2 rounded border border-line-strong px-1.5 py-0.5 text-[10px] text-ink-soft md:block">⌘K</kbd>
      </div>

      <div className="header-actions ml-auto flex shrink-0 items-center gap-1.5 md:ml-0">
        <button
          type="button"
          onClick={onNewNote}
          disabled={!canWrite}
          aria-label="New note"
          className="primary-button flex min-h-11 min-w-11 items-center justify-center gap-2 rounded-lg px-3.5 text-[13px] font-semibold disabled:opacity-50"
        >
          <IconPlus width={16} height={16} />
          <span className="hidden sm:inline">New note</span>
        </button>
        <button
          type="button"
          onClick={onReload}
          disabled={reloading || source === 'loading'}
          aria-label={reloading ? 'Reloading local database' : reloadLabel}
          title={reloadLabel}
          className="flex min-h-11 min-w-11 items-center justify-center gap-2 rounded-lg px-3 text-[13px] font-medium text-ink-soft hover:bg-paper-2 disabled:opacity-50"
        >
          <IconSync width={16} height={16} className={reloading ? 'animate-spin' : undefined} />
          <span className="hidden sm:inline">{reloading ? 'Reloading…' : source === 'error' ? 'Retry' : 'Reload'}</span>
        </button>
        <button
          type="button"
          onClick={() => settingsRef.current?.showModal()}
          aria-haspopup="dialog"
          aria-label="Settings"
          className="flex min-h-11 min-w-11 items-center justify-center rounded-lg text-ink-soft hover:bg-paper-2"
        >
          <IconGear width={17} height={17} />
        </button>
      </div>

      <dialog ref={settingsRef} aria-labelledby="settings-title" className="fixed inset-0 m-auto max-h-[90dvh] w-[calc(100%_-_2rem)] max-w-[420px] overflow-y-auto rounded-2xl bg-paper-3 p-6 text-ink shadow-xl backdrop:bg-navy/40">
        <div className="mb-3 flex items-center justify-between gap-3">
          <h2 id="settings-title" className="text-[16px] font-semibold">Settings</h2>
          <button type="button" autoFocus onClick={() => settingsRef.current?.close()} className="min-h-11 rounded-[6px] border border-line-strong px-3 text-[13px]">Close</button>
        </div>
        <p className="text-[14px] font-medium">{demo ? 'Demo workspace' : 'Local workspace'}</p>
        <p className="mt-2 text-[13px] text-ink-soft">
          {demo ? 'Synthetic notes for trying the interface. Changes are temporary and are not saved to a database.' : 'Notes are saved to your local database. Remote sync and password protection are not controlled by this interface.'}
        </p>
        <fieldset className="mt-6 border-t border-line pt-4">
          <legend className="text-[13px] font-semibold">Appearance</legend>
          <div className="mt-2 flex gap-2">
            {['light', 'dark', 'system'].map(value => <button key={value} type="button" aria-pressed={theme === value} onClick={() => chooseTheme(value)} className={`min-h-11 flex-1 rounded-lg border px-3 text-[13px] ${theme === value ? 'border-route bg-violet-soft text-ink' : 'border-line text-ink-soft'}`}>{value.charAt(0).toUpperCase() + value.slice(1)}</button>)}
          </div>
        </fieldset>
        <p className="mt-3 border-t border-line pt-3 text-[13px] text-ink-soft">
          {source === 'error' ? 'The local database is unavailable. Close Settings and retry the connection.' : source === 'loading' ? 'Connecting to the local database…' : demo ? 'Connect a local backend to use your own notes.' : 'Use Reload to refresh notes and retry failed local saves. Backend configuration is managed outside this interface.'}
        </p>
      </dialog>
    </header>
  )
}
