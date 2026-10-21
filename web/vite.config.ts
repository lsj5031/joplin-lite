import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import { defineConfig } from 'vite'
import viteReact from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { licenseNotices } from './license-notices'

// Optional browser SPA served from disk. The native desktop embeds no SPA.
export default defineConfig({
  server: {
    port: 3000,
  },
  resolve: {
    tsconfigPaths: true,
  },
  plugins: [
    licenseNotices(),
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
