import { existsSync, readFileSync, readdirSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import type { Plugin } from 'vite'
import { z } from 'zod'

const packageMetadata = z.object({ name: z.string(), version: z.string() })
const licenseName = /^(?:licen[cs]e|copying|notice)(?:[._-].*)?$/i
const projectRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

// Notices follow the actual bundler module graph, rather than npm's dev flag.
export function licenseNotices(): Plugin {
  return {
    name: 'joplin-lite-license-notices',
    apply: 'build',
    generateBundle() {
      const packages = new Map<string, { readonly name: string; readonly version: string }>()
      for (const id of this.getModuleIds()) {
        if (!id.includes('node_modules') || id.includes('\0')) continue
        let dir = path.dirname(id.split('?')[0] ?? id)
        while (dir !== path.dirname(dir)) {
          const manifest = path.join(dir, 'package.json')
          if (existsSync(manifest)) {
            const parsed = packageMetadata.safeParse(JSON.parse(readFileSync(manifest, 'utf8')))
            if (parsed.success) {
              packages.set(dir, parsed.data)
              break
            }
          }
          dir = path.dirname(dir)
        }
      }
      let notices = 'Joplin Lite browser third-party notices\n\n'
      for (const [dir, info] of [...packages].sort((a, b) => a[1].name.localeCompare(b[1].name))) {
        const licenses = readdirSync(dir, { withFileTypes: true })
          .filter(entry => entry.isFile() && licenseName.test(entry.name))
        if (licenses.length === 0) throw new Error(`No license file found for bundled package ${info.name}`)
        notices += `\n${info.name}@${info.version}\n${'='.repeat(info.name.length + info.version.length + 1)}\n`
        for (const license of licenses) {
          notices += `\n${license.name}\n\n${readFileSync(path.join(dir, license.name), 'utf8')}\n`
        }
      }
      this.emitFile({ type: 'asset', fileName: 'THIRD_PARTY_NOTICES.txt', source: notices })
      for (const filename of ['LICENSE', 'NOTICE']) {
        this.emitFile({ type: 'asset', fileName: `${filename}.txt`, source: readFileSync(path.join(projectRoot, filename), 'utf8') })
      }
    },
  }
}
