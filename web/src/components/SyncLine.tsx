import type { SyncState } from '~/lib/mock-data'

const STATE_COLOR: Record<SyncState, string> = {
  synced: 'var(--color-moss-bright)',
  syncing: 'var(--color-route-soft)',
  conflict: 'var(--color-signal-bright)',
  error: 'var(--color-amber-bright)',
  locked: 'var(--color-amber-bright)',
}

type Props = {
  state: SyncState
  compact?: boolean
}

/**
 * The sync line — the library's network as service status.
 * Beck grammar: fixed 90°/45° bends, station dots, flat line colors.
 */
export function SyncLine({ state, compact = false }: Props) {
  const c = STATE_COLOR[state]
  const neutral = 'var(--color-navy-line)'
  const running = state === 'syncing'

  return (
    <svg
      viewBox="0 0 720 76"
      className={`h-[52px] w-full ${compact ? '' : 'min-w-[560px]'}`}
      role="img"
      aria-label={`Sync network status: ${state}`}
    >
      {/* clipper stub — feeds this device */}
      <path
        d="M80 40 L140 62 L232 62"
        stroke={neutral}
        strokeWidth="3"
        fill="none"
        strokeLinecap="round"
      />
      <circle cx="232" cy="62" r="3.5" fill={neutral} />
      <text x="242" y="66" className="mono" fontSize="11" fill="var(--color-navy-text)">
        Clipper :41184
      </text>

      {/* mobile branch — 45° off the main line through the target */}
      <path
        d="M360 32 L392 12 L468 12"
        stroke={state === 'synced' ? 'var(--color-moss-bright)' : neutral}
        strokeWidth="3"
        fill="none"
        strokeLinecap="round"
      />
      <circle cx="474" cy="12" r="4.5" fill="var(--color-navy-2)" stroke={state === 'synced' ? 'var(--color-moss-bright)' : neutral} strokeWidth="2.5" />
      <text x="486" y="16" className="mono" fontSize="11" fill="var(--color-navy-text)">
        Joplin mobile
      </text>

      {/* main line: this device → target → desktop */}
      <path d="M80 32 L620 32" stroke={neutral} strokeWidth="3" fill="none" strokeLinecap="round" />
      <path
        d="M80 32 L360 32"
        stroke={c}
        strokeWidth="3"
        fill="none"
        strokeLinecap="round"
      />
      {running && (
        <path
          d="M80 32 L360 32"
          stroke="var(--color-paper)"
          strokeWidth="3"
          fill="none"
          strokeLinecap="round"
          className="service-run"
        />
      )}
      {state === 'conflict' && (
        // disruption marker at the target station
        <g>
          <path d="M352 24 L368 40 M368 24 L352 40" stroke="var(--color-signal-bright)" strokeWidth="2.25" strokeLinecap="round" />
        </g>
      )}

      {/* stations */}
      <circle cx="80" cy="32" r="5.5" fill="var(--color-navy-2)" stroke={c} strokeWidth="2.75" className={running ? 'station-pulse' : undefined} />
      <circle cx="360" cy="32" r="5.5" fill="var(--color-navy-2)" stroke={c} strokeWidth="2.75" />
      <circle cx="620" cy="32" r="4.5" fill="var(--color-navy-2)" stroke={neutral} strokeWidth="2.5" />

      <text x="80" y="56" textAnchor="middle" className="mono" fontSize="11" fill="var(--color-paper)" fontWeight="500">
        joplin-lite
      </text>
      <text x="360" y="56" textAnchor="middle" className="mono" fontSize="11" fill="var(--color-paper)" fontWeight="500">
        Dropbox · target 7
      </text>
      <text x="620" y="56" textAnchor="middle" className="mono" fontSize="11" fill="var(--color-navy-text)">
        Joplin desktop
      </text>
    </svg>
  )
}
