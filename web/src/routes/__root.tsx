/// <reference types="vite/client" />
import { HeadContent, Scripts, createRootRoute } from '@tanstack/react-router'
import * as React from 'react'
import { DefaultCatchBoundary } from '~/components/DefaultCatchBoundary'
import { NotFound } from '~/components/NotFound'
import appCss from '~/styles/app.css?url'

export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      // The webview adopts document.title as the native window title; SH-01
      // expects the product name there, not the npm-style slug.
      { title: 'Joplin Lite' },
      {
        name: 'description',
        content: 'A little room to think. A lightweight workspace for your local notes and Markdown.',
      },
    ],
    links: [
      { rel: 'stylesheet', href: '/fonts/fonts.css' },
      { rel: 'stylesheet', href: appCss },
      { rel: 'icon', type: 'image/svg+xml', href: '/logo.svg' },
      { rel: 'icon', type: 'image/png', sizes: '32x32', href: '/favicon-32x32.png' },
      { rel: 'icon', type: 'image/png', sizes: '16x16', href: '/favicon-16x16.png' },
      { rel: 'apple-touch-icon', href: '/apple-touch-icon.png' },
      { rel: 'manifest', href: '/site.webmanifest' },
    ],
  }),
  errorComponent: DefaultCatchBoundary,
  notFoundComponent: () => <NotFound />,
  shellComponent: RootDocument,
})

// Resolve the theme before React hydrates so the attribute the inline head
// script already wrote matches the client's first render. Server/prerender
// renders the light default; the browser recomputes from storage/OS.
function initialTheme(): 'light' | 'dark' {
  if (typeof window === 'undefined') return 'light'
  try {
    const stored = window.localStorage.getItem('joplin-lite:theme')
    if (stored === 'light' || stored === 'dark') return stored
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  } catch { return 'light' }
}

function RootDocument({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" data-theme={initialTheme()} suppressHydrationWarning>

      <head>
        <script dangerouslySetInnerHTML={{ __html: `try{let t=localStorage.getItem('joplin-lite:theme');document.documentElement.dataset.theme=t==='light'||t==='dark'?t:matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light'}catch{document.documentElement.dataset.theme='light'}` }} />
        <HeadContent />
      </head>
      <body className="min-h-dvh bg-paper text-ink antialiased">
        {children}
        <Scripts />
      </body>
    </html>
  )
}
