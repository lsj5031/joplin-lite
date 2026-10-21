// Renders the product film from the frames shell/promo_test.go captured.
//
//	CAPTURE_DIR=../tmp/promo go test -run TestCapturePromo ./shell/
//	node qa/promo/render.mjs            # film + poster
//	node qa/promo/render.mjs --stills   # one still per scene, for review
//
// Every frame is drawn by qa/promo/compose.html at an exact time and piped
// into ffmpeg, so the edit is reproducible and never depends on screen
// recording timing.
import { createServer } from 'node:http'
import { createReadStream, existsSync, mkdirSync, statSync } from 'node:fs'
import { extname, join, normalize, resolve, sep } from 'node:path'
import { spawn } from 'node:child_process'
import { fileURLToPath, pathToFileURL } from 'node:url'

const ROOT = resolve(fileURLToPath(new URL('../..', import.meta.url)))

async function loadChromium() {
  try {
    const mod = await import('playwright-core')
    if (mod.chromium) return mod.chromium
  } catch {}
  throw new Error('playwright-core not found; install it beside this script or set NODE_PATH')
}

const PROMO = process.env.PROMO_DIR ? resolve(process.env.PROMO_DIR) : join(ROOT, 'tmp/promo')
const OUT = process.env.PROMO_OUT ? resolve(process.env.PROMO_OUT) : join(ROOT, 'docs/promo')
const STILLS = join(PROMO, 'stills')
const CHROME = process.env.CHROME_PATH ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const FFMPEG = process.env.FFMPEG ?? (existsSync('/opt/homebrew/bin/ffmpeg') ? '/opt/homebrew/bin/ffmpeg' : 'ffmpeg')

const args = process.argv.slice(2)
const W = 1920, H = 1080

// Captures may live outside the repository (tmp/, a scratch disk), so frames
// are served under their own prefix instead of a path relative to ROOT.
const FRAME_BASE = '/__promo'
const FRAME_PREFIX = FRAME_BASE + '/'
const FRAME_PATH = FRAME_BASE

function within(root, path) {
  return path === root || path.startsWith(root + sep)
}

const TYPES = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.png': 'image/png',
  '.ttf': 'font/ttf',
  '.svg': 'image/svg+xml',
}

export function serve() {
  const server = createServer((req, res) => {
    let url
    try {
      url = decodeURIComponent((req.url || '/').split('?')[0])
    } catch {
      res.writeHead(400).end('invalid path')
      return
    }
    const frames = url.startsWith(FRAME_PREFIX)
    const root = frames ? PROMO : ROOT
    const path = normalize(join(root, frames ? url.slice(FRAME_PREFIX.length) : url))
    if (!within(root, path) || !existsSync(path) || !statSync(path).isFile()) {
      res.writeHead(404).end('not found')
      return
    }
    res.writeHead(200, {
      'content-type': TYPES[extname(path)] ?? 'application/octet-stream',
      'cache-control': 'no-store',
    })
    createReadStream(path).pipe(res)
  })
  return new Promise((ok) => server.listen(0, '127.0.0.1', () => ok(server)))
}

function encode(out, fps) {
  const ff = spawn(FFMPEG, [
    '-y', '-hide_banner', '-loglevel', 'error',
    '-f', 'image2pipe', '-framerate', String(fps), '-i', '-',
    // The UI is mostly flat color and thin hairlines: keep the encode clean
    // enough that type does not shimmer.
    '-c:v', 'libx264', '-preset', 'slow', '-crf', '16',
    '-pix_fmt', 'yuv420p', '-profile:v', 'high', '-level', '4.2',
    '-g', String(fps * 2), '-movflags', '+faststart',
    out,
  ])
  ff.stderr.on('data', (d) => process.stderr.write(`ffmpeg: ${d}`))
  const done = new Promise((ok, fail) => {
    ff.on('close', (code) => (code === 0 ? ok() : fail(new Error(`ffmpeg exited ${code}`))))
  })
  const write = (buf) => new Promise((ok) => {
    if (ff.stdin.write(buf)) ok()
    else ff.stdin.once('drain', ok)
  })
  return { write, end: () => { ff.stdin.end(); return done } }
}

async function openCut(browser) {
  const context = await browser.newContext({ viewport: { width: W, height: H }, deviceScaleFactor: 1 })
  const page = await context.newPage()
  page.on('pageerror', (e) => console.error('page error:', e.message))
  await page.goto(`http://127.0.0.1:${server.address().port}/qa/promo/compose.html?frames=${FRAME_PATH}`)
  await page.waitForFunction(() => window.__ready === true || window.__bootError, null, { timeout: 30000 })
  const bootError = await page.evaluate(() => window.__bootError)
  if (bootError) throw new Error(bootError)
  return { context, page }
}

async function renderFilm(browser) {
  const mp4 = join(OUT, 'joplin-lite-promo.mp4')
  const { context, page } = await openCut(browser)
  const duration = await page.evaluate(() => window.__duration)
  const fps = await page.evaluate(() => window.__fps)
  const total = Math.round(duration * fps)
  console.log(`film: ${duration.toFixed(2)}s, ${total} frames -> ${mp4}`)

  const ff = encode(mp4, fps)
  for (let i = 0; i < total; i++) {
    await page.evaluate((t) => window.renderAt(t), i / fps)
    await ff.write(await page.screenshot({ type: 'png' }))
    if (i % 150 === 0) process.stdout.write(`  ${i}/${total}\n`)
  }
  await ff.end()
  await context.close()
  return mp4
}

// Stills are the review surface: one frame per scene, written as PNG so the
// typography and workspace can be inspected at full size.
async function renderStills(browser) {
  mkdirSync(STILLS, { recursive: true })
  const { context, page } = await openCut(browser)
  const scenes = await page.evaluate(() => window.__scenes)
  const sampleAt = {
    hook: 0.78,
    library: 0.45,
    write: 0.92,
    palette: 0.82,
    night: 0.85,
    end: 0.78,
  }
  for (const [i, s] of scenes.entries()) {
    const share = sampleAt[s.beat || s.kind] ?? 0.66
    await page.evaluate((tt) => window.renderAt(tt), s.start + s.dur * share)
    await page.screenshot({ path: join(STILLS, `${String(i + 1).padStart(2, '0')}.png`) })
  }
  await context.close()
}

// The poster is the preview still shown before playback: the real 3-pane
// workspace with the notebooks & tags rail open alongside the rendered note.
async function renderPoster(browser) {
  const { context, page } = await openCut(browser)
  const scenes = await page.evaluate(() => window.__scenes)
  const lib = scenes.find((s) => s.beat === 'library') || scenes[1]
  await page.evaluate((t) => window.renderAt(t), lib.start + lib.dur * 0.45)
  const out = join(OUT, 'joplin-lite-promo-poster.png')
  await page.screenshot({ path: out })
  await context.close()
  return out
}

let server
async function main() {
  const chromium = await loadChromium()
  if (existsSync(join(PROMO, 'promo.json'))) {
    console.log(`manifest: ${join(PROMO, 'promo.json')}`)
  } else {
    throw new Error(`no capture in ${PROMO}; run the capture test first`)
  }
  server = await serve()
  let browser
  try {
    browser = await chromium.launch({ headless: true, executablePath: CHROME })
    if (args.includes('--stills')) {
      await renderStills(browser)
    } else {
      mkdirSync(OUT, { recursive: true })
      const out = await renderFilm(browser)
      const { size } = statSync(out)
      console.log(`film: ${(size / 1e6).toFixed(1)} MB`)
      console.log(`poster: ${await renderPoster(browser)}`)
    }
  } finally {
    if (browser) await browser.close()
    server.close()
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  await main()
}
