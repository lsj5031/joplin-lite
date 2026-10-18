import { StrictMode, startTransition } from 'react'
import { hydrateRoot } from 'react-dom/client'
import { StartClient } from '@tanstack/react-start/client'

// Static-SPA client entry.
//
// This build intentionally ships a non-server-rendered shell (see
// vite.config.ts, `spa.enabled`): index.html carries only the document and the
// router bootstrap, so the first client render is what produces the app. React
// still hydrates the document because `hydrateRoot(document, …)` is the only
// document API it offers, detects that the shell has no matching markup, and
// recovers by rendering the client tree — correct, but it reports a
// recoverable hydration error that reads like a crash in the console and in
// error monitoring. Swallow exactly that expected recovery here and surface
// everything else.
startTransition(() => {
  hydrateRoot(
    document,
    <StrictMode>
      <StartClient />
    </StrictMode>,
    {
      onRecoverableError(error) {
        const message = error instanceof Error ? error.message : String(error)
        if (/#418\b/.test(message)) return
        console.error(error)
      },
    },
  )
})
