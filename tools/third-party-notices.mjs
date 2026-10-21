// Run from any directory and redirect stdout to THIRD_PARTY_NOTICES.md.
// This inventories reachable Go packages, not every unused go.mod entry.
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { existsSync, readFileSync, readdirSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const licenseName = /^(?:licen[cs]e|copying|notice|ofl|patents)(?:[._-].*)?$/i
const graphFormat = '{{if .Module}}{{.ImportPath}}\t{{.Dir}}\t{{.Module.Path}}\t{{.Module.Version}}\t{{.Module.Dir}}{{end}}'
const files = new Map()

for (const [cwd, extra] of [
  [root, { GOOS: 'windows', GOARCH: 'amd64', GOFLAGS: '-tags=s3' }],
  [path.join(root, 'shell'), { GOOS: 'windows', GOARCH: 'amd64' }],
  [path.join(root, 'shell'), { GOOS: 'darwin', GOARCH: 'arm64' }],
]) {
  const output = execFileSync('go', ['list', '-deps', '-f', graphFormat, './...'], {
    cwd,
    env: { ...process.env, CGO_ENABLED: '0', ...extra },
    encoding: 'utf8',
  })
  for (const line of output.split(/\r?\n/).filter(Boolean)) {
    const [, directory, module, version, moduleRoot] = line.split('\t')
    if (!module || module.startsWith('github.com/lsj5031/joplin-lite')) continue
    if (!directory || !moduleRoot) throw new Error(`Incomplete Go module metadata: ${module}`)
    const owner = `${module}@${version}`
    let dir = path.resolve(directory)
    const boundary = path.resolve(moduleRoot)
    while (dir.startsWith(boundary)) {
      for (const entry of readdirSync(dir, { withFileTypes: true })) {
        if (!entry.isFile() || !licenseName.test(entry.name) || entry.name === 'LICENSE-LOGO') continue
        const filename = path.join(dir, entry.name)
        const item = files.get(filename) ?? { owners: new Set(), source: `${owner}/${path.relative(boundary, filename).replaceAll('\\', '/')}` }
        item.owners.add(owner)
        files.set(filename, item)
      }
      if (dir === boundary) break
      dir = path.dirname(dir)
    }
  }
}

const goroot = execFileSync('go', ['env', 'GOROOT'], {
  cwd: path.join(root, 'shell'), encoding: 'utf8',
}).trim()
for (const name of ['LICENSE', 'PATENTS']) {
  const filename = path.join(goroot, name)
  if (existsSync(filename)) files.set(filename, { owners: new Set(['Go toolchain and standard library']), source: `Go/${name}` })
}
files.set(path.join(root, 'shell/fonts/OFL.txt'), {
  owners: new Set(['Overpass and Overpass Mono fonts']), source: 'shell/fonts/OFL.txt',
})

const texts = []
for (const [filename, item] of files) {
  texts.push({ ...item, text: readFileSync(filename, 'utf8').replaceAll('\r\n', '\n').replace(/[ \t]+$/gm, '').trimEnd() })
}
// These notices cover embedded artwork and development tooling in Git history.
// Historical tooling is not included in application binaries.
for (const [owner, source] of [
  ['MingCute icon embedded in MyGO ui/combobox.go (Apache-2.0)', 'https://raw.githubusercontent.com/Richard9394/MingCute/ca98bb55513a22ff0dab713aaf7870b512944653/LICENSE'],
  ['Historical Impeccable development tooling (Apache-2.0)', 'https://raw.githubusercontent.com/pbakaus/impeccable/main/LICENSE'],
  ['Historical modern-screenshot development tooling (MIT)', 'https://raw.githubusercontent.com/qq15725/modern-screenshot/master/LICENSE'],
  ['TanStack starter source and historical starter artwork (MIT)', 'https://raw.githubusercontent.com/TanStack/router/main/LICENSE'],
]) {
  const response = await fetch(source, { signal: AbortSignal.timeout(30_000) })
  if (!response.ok) throw new Error(`Cannot retrieve upstream notice: ${source} (${response.status})`)
  texts.push({ owners: new Set([owner]), source, text: (await response.text()).replaceAll('\r\n', '\n').trimEnd() })
}

// Identical license texts are stored once; every component retains its attribution.
const groups = new Map()
for (const item of texts) {
  const hash = createHash('sha256').update(item.text).digest('hex')
  const group = groups.get(hash) ?? { text: item.text, entries: [] }
  group.entries.push({ source: item.source, owners: [...item.owners].sort() })
  groups.set(hash, group)
}
const components = [...new Set(texts.flatMap(item => [...item.owners]))].sort()
let output = `# Third-party notices

Joplin Lite application code is AGPL-3.0-or-later; see LICENSE and NOTICE.
The components below retain their upstream licenses and copyright notices.

This inventory covers the CLI (including optional S3), native Windows/macOS
package imports, embedded fonts, and identified historical development tooling.
Some upstream aggregate notices describe additional source components; inclusion
does not assert that every described component is linked into every binary.
The optional browser build emits its own THIRD_PARTY_NOTICES.txt.

Regenerate with \`node tools/third-party-notices.mjs > THIRD_PARTY_NOTICES.md\`
after dependency updates, and review new or changed license terms.

## Components

${components.map(name => `- ${name}`).join('\n')}

## Verbatim upstream license and notice texts
`
let index = 0
for (const group of groups.values()) {
  output += `\n### Notice ${++index}\n\n`
  for (const entry of group.entries) {
    output += `Applies to: ${entry.owners.join(', ')}\n\nSource: ${entry.source}\n\n`
  }
  output += `~~~text\n${group.text}\n~~~\n`
}
process.stdout.write(output)
