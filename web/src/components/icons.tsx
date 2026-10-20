const base = {
  width: 16,
  height: 16,
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.75,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
  'aria-hidden': true,
}

export const IconSearch = (p: React.SVGProps<SVGSVGElement>) => (
  <svg {...base} {...p}>
    <circle cx="11" cy="11" r="6.5" />
    <path d="m16 16 4.5 4.5" />
  </svg>
)

export const IconSync = (p: React.SVGProps<SVGSVGElement>) => (
  <svg {...base} {...p}>
    <path d="M20 12a8 8 0 0 1-13.7 5.6M4 12a8 8 0 0 1 13.7-5.6" />
    <path d="M17.5 3v3.6h-3.6M6.5 21v-3.6h3.6" />
  </svg>
)

export const IconLock = (p: React.SVGProps<SVGSVGElement>) => (
  <svg {...base} {...p}>
    <rect x="5" y="10.5" width="14" height="9.5" rx="2" />
    <path d="M8.5 10.5V8a3.5 3.5 0 0 1 7 0v2.5" />
  </svg>
)

export const IconUnlock = (p: React.SVGProps<SVGSVGElement>) => (
  <svg {...base} {...p}>
    <rect x="5" y="10.5" width="14" height="9.5" rx="2" />
    <path d="M8.5 10.5V8a3.5 3.5 0 0 1 6.8-1.2" />
  </svg>
)

export const IconGear = (p: React.SVGProps<SVGSVGElement>) => (
  <svg {...base} {...p}>
    <circle cx="12" cy="12" r="3" />
    <path d="M12 3.5v2.2M12 18.3v2.2M20.5 12h-2.2M5.7 12H3.5M18 6l-1.6 1.6M7.6 16.4 6 18M18 18l-1.6-1.6M7.6 7.6 6 6" />
  </svg>
)

export const IconPlus = (p: React.SVGProps<SVGSVGElement>) => (
  <svg {...base} {...p}>
    <path d="M12 5v14M5 12h14" />
  </svg>
)

export const IconChevron = (p: React.SVGProps<SVGSVGElement>) => (
  <svg {...base} {...p}>
    <path d="m9 6 6 6-6 6" />
  </svg>
)

export const IconNotebook = (p: React.SVGProps<SVGSVGElement>) => (
  <svg {...base} {...p}>
    <path d="M7 4h11a1 1 0 0 1 1 1v15a1 1 0 0 1-1 1H7" />
    <path d="M7 4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2M10 8h6M10 12h6" />
  </svg>
)

export const IconTag = (p: React.SVGProps<SVGSVGElement>) => (
  <svg {...base} {...p}>
    <path d="M4 11.5V5a1 1 0 0 1 1-1h6.5a1 1 0 0 1 .7.3l8 8a1 1 0 0 1 0 1.4l-6.5 6.5a1 1 0 0 1-1.4 0l-8-8a1 1 0 0 1-.3-.7Z" />
    <circle cx="8.5" cy="8.5" r="1.25" />
  </svg>
)

export const IconTodo = (p: React.SVGProps<SVGSVGElement>) => (
  <svg {...base} {...p}>
    <rect x="4.5" y="4.5" width="15" height="15" rx="2" />
    <path d="m8.5 12 2.5 2.5 4.5-5" />
  </svg>
)

export const IconConflict = (p: React.SVGProps<SVGSVGElement>) => (
  <svg {...base} {...p}>
    <path d="M12 3.5 20.5 12 12 20.5 3.5 12 12 3.5Z" />
    <path d="M12 8.5v4M12 15.5h.01" />
  </svg>
)

export const IconBack = (p: React.SVGProps<SVGSVGElement>) => (
  <svg {...base} {...p}>
    <path d="M15 6l-6 6 6 6" />
  </svg>
)

export const IconMonitor = (p: React.SVGProps<SVGSVGElement>) => (
  <svg {...base} {...p}>
    <rect x="3.5" y="5" width="17" height="11" rx="1.5" />
    <path d="M9 20h6M12 16v4" />
  </svg>
)

export const IconPhone = (p: React.SVGProps<SVGSVGElement>) => (
  <svg {...base} {...p}>
    <rect x="7.5" y="3.5" width="9" height="17" rx="1.5" />
    <path d="M11 17.5h2" />
  </svg>
)

export const IconCloud = (p: React.SVGProps<SVGSVGElement>) => (
  <svg {...base} {...p}>
    <path d="M7 18a4 4 0 0 1-.4-8A5.5 5.5 0 0 1 17.3 11 3.5 3.5 0 0 1 17 18H7Z" />
  </svg>
)

export const IconClipper = (p: React.SVGProps<SVGSVGElement>) => (
  <svg {...base} {...p}>
    <circle cx="6.5" cy="17.5" r="2.5" />
    <circle cx="17.5" cy="17.5" r="2.5" />
    <path d="M8.4 15.6 18 4M15.6 15.6 6 4" />
  </svg>
)

/** The folded-ribbon J mark, shared with the favicon and platform icons. */
export const Mark = (p: React.SVGProps<SVGSVGElement>) => (
  <svg width="36" height="36" viewBox="0 0 64 64" fill="none" className="text-route" aria-hidden {...p}>
    <path d="M22 8h28L38 20H22z" fill="currentColor" opacity=".36" />
    <path d="M38 20 50 8v35H38z" fill="currentColor" opacity=".74" />
    <path d="M14 43h36L38 55H26z" fill="currentColor" />
    <path d="M26 31v12H14z" fill="currentColor" opacity=".56" />
    <path d="M14 31h12L14 43z" fill="currentColor" opacity=".68" />
  </svg>
)
