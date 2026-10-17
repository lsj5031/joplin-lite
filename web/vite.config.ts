import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import { defineConfig } from 'vite'
import viteReact from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// Static SPA, not SSR: the desktop shell embeds `dist/` in the Go binary and
// serves it at mygo://localhost. There is no Node runtime in production and no
// hydration step, so the UI can read the local database through the typed
// bindings without a server round-trip for first paint.
export default defineConfig({
  server: {
    port: 3000,
  },
  resolve: {
    tsconfigPaths: true,
  },
  plugins: [
    tailwindcss(),
    tanstackStart({
      srcDirectory: 'src',
      spa: {
        enabled: true,
        // Write the app shell to index.html rather than Start's _shell.html:
        // the embedded static server uses index.html as its client-route
        // fallback for every path the webview asks for.
        prerender: {
          outputPath: '/index',
        },
      },
    }),
    viteReact(),
  ],
})
