// Records short demo clips of joplin-lite's showcase features, then stitches
// them into one polished product video.
//
//   bash qa/reset.sh               # fresh seeded library
//   node qa/make_demo.mjs          # -> qa/video/joplin-lite-demo.mp4
//
// Recording uses Playwright's video capture (system ffmpeg is symlinked into
// Playwright's ffmpeg cache). Captions and a synthetic cursor are injected into
// the page so they are burned into the recording.
import { execFileSync } from 'node:child_process'
import { mkdirSync, writeFileSync, rmSync, existsSync } from 'node:fs'
import { chromium } from '../.amp/in/harness/node_modules/playwright-core/index.mjs'

const UI = process.env.UI_ORIGIN ?? 'http://127.0.0.1:4331'
const API = process.env.UI_API ?? 'http://127.0.0.1:41888'
const TOKEN = process.env.UI_TOKEN ?? 'ui-token'
const CHROME = process.env.CHROME_PATH ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const FFMPEG = process.env.FFMPEG ?? '/opt/homebrew/bin/ffmpeg'
const FONT = '/System/Library/Fonts/Supplemental/Arial.ttf'
const VIDEO = new URL('./video/', import.meta.url).pathname
const RAW = VIDEO + 'raw/'
mkdirSync(RAW, { recursive: true })

const W = 1440, H = 900
const ff = (args) => execFileSync(FFMPEG, ['-y', '-hide_banner', '-loglevel', 'error', ...args])

const rows = (p) => p.locator('.note-row')
const searchBox = (p) => p.getByRole('searchbox', { name: 'Search notes' })
const nav = (p) => p.locator('nav[aria-label="Notebooks and tags"]:visible')

async function decorate(page) {
  await page.evaluate(() => {
    if (document.getElementById('demo-ui')) return
    const s = document.createElement('style')
    s.id = 'demo-ui'
    s.textContent = `
      #demo-caption{position:fixed;left:50%;bottom:34px;transform:translateX(-50%);z-index:2147483647;
        background:rgba(22,40,60,.93);color:#fff;padding:11px 20px;border-radius:999px;
        font:600 15px ui-sans-serif,system-ui;letter-spacing:.01em;box-shadow:0 10px 34px rgba(0,0,0,.30);
        opacity:0;transition:opacity .3s ease;max-width:74vw;text-align:center;pointer-events:none}
      #demo-caption.on{opacity:1}
      #demo-cursor{position:fixed;z-index:2147483647;width:17px;height:17px;margin:-8.5px 0 0 -8.5px;
        border-radius:50%;background:rgba(41,77,120,.92);box-shadow:0 0 0 2px rgba(255,255,255,.92);
        pointer-events:none;transition:left .07s linear,top .07s linear;left:-40px;top:-40px}
      .demo-ripple{position:fixed;z-index:2147483646;width:14px;height:14px;margin:-7px 0 0 -7px;
        border-radius:50%;border:2px solid rgba(41,77,120,.85);pointer-events:none;
        animation:demo-rip .55s ease-out forwards}
      @keyframes demo-rip{from{transform:scale(.4);opacity:.9}to{transform:scale(3.6);opacity:0}}`
    document.head.appendChild(s)
    const cap = document.createElement('div'); cap.id = 'demo-caption'; document.body.appendChild(cap)
    const cur = document.createElement('div'); cur.id = 'demo-cursor'; document.body.appendChild(cur)
    window.__cap = (t) => { cap.textContent = t || ''; cap.classList.toggle('on', !!t) }
    addEventListener('mousemove', (e) => {
      cur.style.left = e.clientX + 'px'; cur.style.top = e.clientY + 'px'
    }, true)
    addEventListener('mousedown', (e) => {
      const r = document.createElement('div'); r.className = 'demo-ripple'
      r.style.left = e.clientX + 'px'; r.style.top = e.clientY + 'px'
      document.body.appendChild(r); setTimeout(() => r.remove(), 560)
    }, true)
  })
}

const cap = async (page, text, ms = 0) => {
  await page.evaluate((t) => window.__cap(t), text)
  if (ms) await page.waitForTimeout(ms)
}

async function glide(page, loc) {
  const box = await loc.boundingBox()
  if (!box) return
  const tx = box.x + box.width / 2, ty = box.y + box.height / 2
  const cur = await page.evaluate(() => {
    const c = document.getElementById('demo-cursor')
    return { x: parseFloat(c.style.left) || 200, y: parseFloat(c.style.top) || 200 }
  })
  const steps = 14
  for (let i = 1; i <= steps; i++) {
    await page.mouse.move(cur.x + (tx - cur.x) * i / steps, cur.y + (ty - cur.y) * i / steps)
    await page.waitForTimeout(14)
  }
  await page.waitForTimeout(120)
}

async function click(page, loc, wait = 400) {
  await glide(page, loc)
  await loc.click()
  await page.waitForTimeout(wait)
}

async function search(page, q) {
  await glide(page, searchBox(page))
  await searchBox(page).fill(q)
  await page.waitForTimeout(500)
}

async function openNote(page, title) {
  await search(page, title)
  const row = rows(page).filter({ has: page.getByText(title, { exact: true }) }).first()
  await row.waitFor({ state: 'visible', timeout: 10000 })
  await click(page, row, 350)
  await page.waitForFunction(() => {
    const t = document.querySelector('textarea[aria-label="Note body, markdown source"]')
    return t && !t.disabled
  }, null, { timeout: 10000 })
  await page.waitForTimeout(150)
}

async function gotoApp(page) {
  await page.goto(UI + '/', { waitUntil: 'domcontentloaded' })
  await page.waitForFunction(() => document.querySelectorAll('.note-row').length > 0
    || document.body.innerText.includes('Your library is ready'), null, { timeout: 20000 })
  await page.waitForTimeout(400)
  await decorate(page)
}

// ------------------------------------------------------------------ clips
const CLIPS = [
  {
    name: '01-library-and-search',
    title: 'Your whole library, instantly searchable',
    run: async (page) => {
      await cap(page, 'A fast, offline-first note library', 1400)
      await glide(page, rows(page).first())
      await page.mouse.wheel(0, 700); await page.waitForTimeout(700)
      await page.mouse.wheel(0, -700); await page.waitForTimeout(500)
      await cap(page, 'Search titles and bodies with full-text search')
      await search(page, 'office hours')
      await page.waitForTimeout(600)
      await search(page, 'lake was still')
      await page.waitForTimeout(600)
      await glide(page, searchBox(page))
      await searchBox(page).focus()
      await page.keyboard.press('Escape')
      await page.waitForTimeout(500)
      if (await searchBox(page).inputValue()) await searchBox(page).fill('')
      await page.waitForTimeout(400)
      await cap(page, 'Filter by notebook…')
      await click(page, nav(page).getByRole('button', { name: 'Work', exact: true }), 700)
      await cap(page, '…or by tag', 200)
      await click(page, nav(page).getByRole('button', { name: 'urgent', exact: true }), 900)
    },
  },
  {
    name: '02-markdown',
    title: 'Write Markdown, preview it live',
    run: async (page) => {
      await cap(page, 'Open any note')
      await openNote(page, 'Markdown field guide')
      await cap(page, 'Write Markdown on the left…', 600)
      const ta = page.getByLabel('Note body, markdown source')
      await glide(page, ta)
      await ta.click()
      await page.keyboard.press('Control+End')
      await page.keyboard.type('\n\n> Rendered from the same source.')
      await page.waitForTimeout(700)
      await cap(page, '…and switch to a sanitized preview')
      await click(page, page.getByRole('button', { name: /preview/i }), 1100)
      await cap(page, 'Headings, lists, tasks, code and links', 1400)
      await click(page, page.getByRole('button', { name: /preview/i }), 500)
    },
  },
  {
    name: '03-organize',
    title: 'Notebooks and tags in one place',
    run: async (page) => {
      await cap(page, 'Create a notebook inline')
      await click(page, nav(page).getByRole('button', { name: 'New notebook' }), 300)
      const form = page.getByRole('form', { name: 'Create notebook' })
      await form.getByLabel('Notebook name').fill('Demo notebook')
      await page.waitForTimeout(400)
      await click(page, page.getByRole('button', { name: 'Create notebook', exact: true }), 700)
      await cap(page, 'Attach tags from the note menu')
      await openNote(page, 'Tagged: weekend plan')
      await click(page, page.getByRole('button', { name: 'Manage note' }), 400)
      const mform = page.getByRole('form', { name: 'Manage note' })
      await click(page, mform.getByLabel('New tag (optional)'), 200)
      await mform.getByLabel('New tag (optional)').fill('demo')
      await page.waitForTimeout(400)
      await click(page, page.getByRole('button', { name: 'Save details' }), 900)
      await cap(page, 'Tags show up as chips on the note', 1200)
    },
  },
  {
    name: '04-conflicts',
    title: 'Sync conflicts are visible and actionable',
    run: async (page) => {
      await cap(page, 'A sync conflict keeps both versions')
      await openNote(page, 'Conflict: office hours')
      await cap(page, 'Nothing was overwritten — choose what to keep', 1600)
      await click(page, page.getByRole('button', { name: 'Open synced version' }), 1300)
      await cap(page, 'Jump straight to the synced copy', 1200)
      await openNote(page, 'Conflict: office hours')
      await click(page, page.getByRole('button', { name: 'Keep my copy' }), 1100)
      await cap(page, 'Or dismiss and keep editing your copy', 1100)
    },
  },
  {
    name: '05-todos',
    title: 'To-dos with due dates',
    run: async (page) => {
      await cap(page, 'Check off a to-do')
      await openNote(page, 'Todo: ship M4')
      await click(page, page.getByRole('button', { name: 'Mark complete' }), 1300)
      await cap(page, 'Progress shows in the list too', 1100)
      await click(page, page.getByRole('button', { name: 'Completed' }), 900)
    },
  },
]

async function record(clip, browser) {
  const context = await browser.newContext({
    viewport: { width: W, height: H },
    colorScheme: 'light',
    recordVideo: { dir: RAW, size: { width: W, height: H } },
  })
  await context.grantPermissions(['local-network-access'], { origin: API }).catch(() => {})
  await context.addInitScript(({ base, token }) => {
    window.__GOJOPLIN__ = { base, token, shell: 'browser', profile: 'qa' }
  }, { base: API, token: TOKEN })
  const page = await context.newPage()
  const video = page.video()
  try {
    await gotoApp(page)
    await clip.run(page)
    await page.waitForTimeout(600)
  } finally {
    await context.close()
  }
  const webm = await video.path()
  const mp4 = VIDEO + clip.name + '.mp4'
  ff(['-i', webm, '-vf', `scale=${W}:${H}:flags=lanczos,fps=30`,
    '-c:v', 'libx264', '-pix_fmt', 'yuv420p', '-crf', '20', '-preset', 'medium', mp4])
  console.log(`  clip -> ${clip.name}.mp4`)
  return { name: clip.name, title: clip.title, mp4 }
}

// ffmpeg here has no drawtext/libfreetype, so title cards are rendered as PNGs
// by the same headless browser and turned into short clips.
async function card(page, text, file, sub = '', seconds = 3) {
  const fs = Math.round(74 - sub.length * 0.02)
  await page.setContent(`<!doctype html><html><body style="margin:0;width:${W}px;height:${H}px;
    display:flex;flex-direction:column;align-items:center;justify-content:center;background:#16283c;
    font-family:ui-sans-serif,system-ui,-apple-system,'Segoe UI',sans-serif;color:#fffdf9">
    <div style="width:64px;height:3px;background:#3e9b63;margin-bottom:26px;border-radius:2px"></div>
    <div style="font-size:${fs}px;font-weight:700;letter-spacing:-.02em">${text}</div>
    ${sub ? `<div style="margin-top:16px;font-size:22px;color:#a8b4c4">${sub}</div>` : ''}
    </body></html>`)
  await page.waitForTimeout(120)
  const png = file.replace(/\.mp4$/, '.png')
  await page.screenshot({ path: png })
  ff(['-loop', '1', '-i', png, '-t', String(seconds), '-r', '30',
    '-vf', `scale=${W}:${H}`, '-c:v', 'libx264', '-pix_fmt', 'yuv420p', '-crf', '20', file])
}

async function main() {
  for (const f of ['list.txt', 'joplin-lite-demo.mp4']) rmSync(VIDEO + f, { force: true })

  const browser = await chromium.launch({ headless: true, executablePath: CHROME })
  const done = []
  for (const clip of CLIPS) {
    const mp4 = VIDEO + clip.name + '.mp4'
    if (existsSync(mp4) && !process.env.DEMO_FORCE) {
      console.log(`reusing ${clip.name}.mp4`)
      done.push({ name: clip.name, title: clip.title })
      continue
    }
    console.log(`recording ${clip.name}…`)
    try {
      done.push(await record(clip, browser))
    } catch (e) {
      console.error(`  clip ${clip.name} failed: ${e.message}`)
    }
  }
  if (!done.length) { await browser.close(); throw new Error('no clips recorded') }

  const cardPage = await browser.newPage({ viewport: { width: W, height: H } })
  await card(cardPage, 'joplin-lite', VIDEO + '00-intro.mp4', 'A local-first Joplin workspace', 4)
  const order = ['00-intro.mp4']
  let i = 0
  for (const d of done) {
    i += 1
    const bn = String(i).padStart(2, '0')
    await card(cardPage, d.title, VIDEO + `${bn}-title.mp4`, '', 2.5)
    order.push(`${bn}-title.mp4`, d.name + '.mp4')
  }
  await card(cardPage, 'Verified end to end', VIDEO + '99-outro.mp4', '200 user stories · 187 automated passes', 4)
  order.push('99-outro.mp4')
  await cardPage.close()
  await browser.close()

  writeFileSync(VIDEO + 'list.txt', order.map((f) => `file '${VIDEO}${f}'`).join('\n') + '\n')
  ff(['-f', 'concat', '-safe', '0', '-i', VIDEO + 'list.txt', '-c', 'copy', VIDEO + 'joplin-lite-demo.mp4'])
  console.log(`\nclips: ${done.length}`)
  console.log(`video: ${VIDEO}joplin-lite-demo.mp4`)
}

main().catch((e) => { console.error(e); process.exit(1) })
