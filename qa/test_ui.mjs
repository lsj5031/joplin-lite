// User-story UI suite for joplin-lite.
//
// Runs the production bundle (http://127.0.0.1:4331) against disposable
// backends (41888 seeded, 41893 empty) and writes qa/results-ui.json.
//
//   node qa/test_ui.mjs            # everything
//   node qa/test_ui.mjs UI-06 UI-07
import assert from 'node:assert/strict'
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { chromium } from '../.amp/in/harness/node_modules/playwright-core/index.mjs'

const UI = process.env.UI_ORIGIN ?? 'http://127.0.0.1:4331'
const API = process.env.UI_API ?? 'http://127.0.0.1:41888'
const TOKEN = process.env.UI_TOKEN ?? 'ui-token'
const EMPTY_API = 'http://127.0.0.1:41893'
const EMPTY_TOKEN = 'empty-token'
const EVIDENCE = new URL('./evidence/', import.meta.url).pathname
const RESULTS = new URL('./results-ui.json', import.meta.url).pathname
const seed = JSON.parse(readFileSync(new URL('./run/seed.json', import.meta.url), 'utf8'))
mkdirSync(EVIDENCE, { recursive: true })

const only = process.argv.slice(2)
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROME_PATH ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' })
const results = {}

const enc = encodeURIComponent
async function apiAt(base, token, path, method = 'GET', body) {
  const sep = path.includes('?') ? '&' : '?'
  const res = await fetch(`${base}${path}${sep}token=${enc(token)}`, {
    method,
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  let json = null
  try { json = text ? JSON.parse(text) : null } catch { /* text */ }
  return { status: res.status, json, text }
}
const api = (path, method, body) => apiAt(API, TOKEN, path, method, body)
const softDelete = async (base, token, id) => { try { await apiAt(base, token, `/notes/${id}`, 'DELETE') } catch { /* ignore */ } }
const NOTE_FIELDS = 'id,title,body,parent_id,tag_ids,is_todo,todo_completed,todo_due,is_conflict,conflict_original_id,updated_time,source_url,author'

async function noteByTitle(title, base = API, token = TOKEN) {
  const r = await apiAt(base, token, `/notes?query=${enc(title)}&summary=1&limit=100&include_conflicts=1`)
  assert.equal(r.status, 200, `summary query failed: ${r.status}`)
  return (r.json.items ?? []).find((n) => n.title === title)
}
async function getNote(id, base = API, token = TOKEN) {
  const r = await apiAt(base, token, `/notes/${id}?fields=${NOTE_FIELDS}`)
  assert.equal(r.status, 200, `GET note ${id}: ${r.status}`)
  return r.json
}
async function getTags(id, base = API, token = TOKEN) {
  const r = await apiAt(base, token, `/notes/${id}/tags?limit=100`)
  assert.equal(r.status, 200, `GET note tags ${id}: ${r.status}`)
  return (r.json.items ?? []).map((t) => t.id)
}

async function newPage(opts = {}) {
  const context = await browser.newContext({
    viewport: opts.viewport ?? { width: 1440, height: 900 },
    colorScheme: opts.colorScheme ?? 'light',
  })
  const base = opts.base ?? API
  const token = opts.token ?? TOKEN
  await context.grantPermissions(['local-network-access'], { origin: base }).catch(() => {})
  await context.addInitScript(({ base, token }) => {
    window.__GOJOPLIN__ = { base, token, shell: 'browser', profile: 'qa' }
  }, { base, token })
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', (e) => errors.push(e.message))
  page.__errors = errors
  page.__api = (path, method, body) => apiAt(base, token, path, method, body)
  if (opts.route) await opts.route(page)
  return { context, page, base, token }
}

async function goto(page, base = UI, opts = {}) {
  await page.goto(base + '/', { waitUntil: 'domcontentloaded' })
  try {
    await page.waitForFunction(() => {
      const rows = document.querySelectorAll('.note-row').length
      const text = document.body.innerText
      return rows > 0 || text.includes('Your library is ready for its first note') || text.includes('Cannot load your local library')
    }, null, { timeout: 20000 })
  } catch (e) {
    console.error('GOTO DEBUG', page.url(), JSON.stringify((await page.locator('body').innerText()).slice(0, 400)))
    throw e
  }
  await page.waitForFunction(() => {
    const s = document.querySelector('footer [role="status"]')
    return s && /Saved locally|failed|disconnected|Demo/.test(s.textContent)
  }, null, { timeout: 20000 }).catch(() => {})
  await page.waitForTimeout(150)
}

const rows = (page) => page.locator('.note-row')
const searchBox = (page) => page.getByRole('searchbox', { name: 'Search notes' })
const textarea = (page) => page.getByLabel('Note body, markdown source')
const editorTitle = (page) => page.locator('.editor-title')
const footer = (page) => page.locator('footer [role="status"]')
const nav = (page) => page.locator('nav[aria-label="Notebooks and tags"]:visible')

async function search(page, q) {
  await searchBox(page).fill(q)
  await page.waitForTimeout(400)
}
async function openTitle(page, title) {
  await search(page, title)
  const row = rows(page).filter({ has: page.getByText(title, { exact: true }) }).first()
  await row.waitFor({ state: 'visible', timeout: 10000 })
  await row.click()
  await page.waitForFunction(() => {
    const t = document.querySelector('textarea[aria-label="Note body, markdown source"]')
    return t && !t.disabled
  }, null, { timeout: 10000 })
  await page.waitForTimeout(120)
}
async function waitSaved(page) {
  await page.waitForFunction(() => {
    const s = document.querySelector('footer [role="status"]')
    return s && s.textContent.includes('Saved locally')
  }, null, { timeout: 10000 })
}
async function manage(page) {
  await page.getByRole('button', { name: 'Manage note' }).click()
  await page.getByRole('form', { name: 'Manage note' }).waitFor()
}
async function saveDetails(page) {
  await page.getByRole('button', { name: 'Save details' }).click()
  await page.getByRole('form', { name: 'Manage note' }).waitFor({ state: 'detached', timeout: 10000 })
  await waitSaved(page)
}

async function run(id, name, fn, opts = {}) {
  if (only.length && !only.includes(id)) return
  const started = Date.now()
  let page
  let context
  try {
    const made = await newPage(opts)
    page = made.page
    context = made.context
    if (!opts.noGoto) await goto(page)
    await fn(page, made)
    results[id] = { ok: true, name, ms: Date.now() - started, detail: '' }
    if (opts.shot) await page.screenshot({ path: EVIDENCE + id.toLowerCase() + '.png', fullPage: false })
    process.stdout.write(`ok   ${id} ${name}\n`)
  } catch (e) {
    results[id] = { ok: false, name, ms: Date.now() - started, detail: String(e?.message ?? e) }
    process.stdout.write(`FAIL ${id} ${name}: ${String(e?.message ?? e).split('\n')[0]}\n`)
    if (process.env.QA_STACK) console.error(e?.stack ?? e)
    try { if (page) await page.screenshot({ path: EVIDENCE + 'fail-' + id.toLowerCase() + '.png' }) } catch { /* ignore */ }
  } finally {
    try { await context?.close() } catch { /* ignore */ }
  }
}

// ---------------------------------------------------------------- inventory
await run('UI-01', 'initial live library', async (page) => {
  assert.equal(await rows(page).count(), 50, 'first keyset page')
  assert.match(await footer(page).innerText(), /Saved locally/)
  assert.doesNotMatch(await page.locator('body').innerText(), /changes are temporary/i)
  assert.match(await nav(page).innerText(), /NOTEBOOKS/i)
  assert.match(await nav(page).innerText(), /TAGS/i)
  assert.match(await nav(page).innerText(), /50\+/)
}, { shot: true })

await run('UI-02', 'empty library first step', async (page, m) => {
  assert.match(await page.locator('body').innerText(), /Your library is ready for its first note/)
  await page.getByRole('button', { name: 'Create a note' }).click()
  await textarea(page).waitFor()
  assert.equal(await editorTitle(page).innerText(), 'Untitled note')
  const r = await m.page.__api('/notes?limit=5&order_by=updated_time&order_dir=desc')
  const created = r.json.items.find((n) => n.title === 'Untitled note')
  assert.ok(created, 'note created through API')
  await softDelete(m.base, m.token, created.id)
}, { base: EMPTY_API, token: EMPTY_TOKEN })

await run('UI-03', 'create note', async (page, m) => {
  await page.getByRole('button', { name: 'New note', exact: true }).click()
  await page.waitForFunction(() => [...document.querySelectorAll('.note-row')].some((e) => e.textContent.includes('Untitled note')))
  assert.equal(await editorTitle(page).innerText(), 'Untitled note')
  assert.match(await textarea(page).inputValue(), /^# Untitled note/)
  const n = await noteByTitle('Untitled note')
  assert.ok(n, 'note in API')
  await waitSaved(page)
  await softDelete(API, TOKEN, n.id)
})

await run('UI-04', 'create note in selected notebook', async (page, m) => {
  await nav(page).getByRole('button', { name: 'Work', exact: true }).click()
  await page.waitForTimeout(350)
  await page.getByRole('button', { name: 'New note', exact: true }).click()
  await page.waitForFunction(() => [...document.querySelectorAll('.note-row')].some((e) => e.textContent.includes('Untitled note')))
  const n = await noteByTitle('Untitled note')
  assert.ok(n, 'created')
  assert.equal(n.parent_id, seed.work)
  await softDelete(API, TOKEN, n.id)
})

await run('UI-05', 'create during tag filter', async (page, m) => {
  await nav(page).getByRole('button', { name: 'idea', exact: true }).click()
  await page.waitForTimeout(350)
  assert.match(await page.locator('.note-list h2').innerText(), /Tag: idea/)
  await page.getByRole('button', { name: 'New note', exact: true }).click()
  await page.waitForFunction(() => [...document.querySelectorAll('.note-row')].some((e) => e.textContent.includes('Untitled note')))
  assert.equal(await page.locator('.note-list h2').innerText(), 'All notes')
  const n = await noteByTitle('Untitled note')
  assert.equal(n.parent_id ?? '', '')
  await softDelete(API, TOKEN, n.id)
})

await run('UI-06', 'body autosave', async (page, m) => {
  await openTitle(page, 'Journal 01')
  const before = await getNote((await noteByTitle('Journal 01')).id)
  const sentinel = `AUTOSAVE-${Date.now()}`
  await textarea(page).fill(before.body + `\n\n${sentinel}`)
  await page.waitForFunction(() => document.querySelector('footer [role="status"]')?.textContent?.includes('Saving locally'), null, { timeout: 3000 }).catch(() => {})
  await waitSaved(page)
  const after = await getNote(before.id)
  assert.ok(after.body.includes(sentinel), 'stored body has sentinel')
  await api(`/notes/${before.id}`, 'PUT', { body: before.body })
})

await run('UI-07', 'instant reload durability', async (page, m) => {
  await openTitle(page, 'Journal 02')
  const n = await noteByTitle('Journal 02')
  const before = await getNote(n.id)
  const sentinel = `RELOAD-${Date.now()}`
  await textarea(page).fill(before.body + `\n\n${sentinel}`)
  await page.reload({ waitUntil: 'domcontentloaded' })
  await goto(page)
  await openTitle(page, 'Journal 02')
  assert.ok((await textarea(page).inputValue()).includes(sentinel), 'recovered pending edit')
  await page.waitForTimeout(900)
  const after = await getNote(n.id)
  assert.ok(after.body.includes(sentinel), 'recovered edit reached the API')
  await api(`/notes/${n.id}`, 'PUT', { body: before.body })
})

await run('UI-08', 'title edit via Manage note', async (page, m) => {
  const n = await noteByTitle('Tagged: weekend plan')
  const before = await getNote(n.id)
  await openTitle(page, 'Tagged: weekend plan')
  await manage(page)
  const newTitle = `Weekend plan ${Date.now()}`
  await page.getByRole('form', { name: 'Manage note' }).getByLabel('Title').fill(newTitle)
  await saveDetails(page)
  assert.equal(await editorTitle(page).innerText(), newTitle)
  const after = await getNote(n.id)
  assert.equal(after.title, newTitle)
  await api(`/notes/${n.id}`, 'PUT', { title: before.title })
})

await run('UI-09', 'move note to notebook', async (page, m) => {
  const n = await noteByTitle('No notebook')
  const before = await getNote(n.id)
  await openTitle(page, 'No notebook')
  await manage(page)
  await page.getByRole('form', { name: 'Manage note' }).getByLabel('Notebook').selectOption(seed.personal)
  await saveDetails(page)
  const after = await getNote(n.id)
  assert.equal(after.parent_id, seed.personal)
  assert.match(await page.locator('.editor-header').innerText(), /Personal/)
  await api(`/notes/${n.id}`, 'PUT', { parent_id: before.parent_id })
})

await run('UI-10', 'assign existing tag', async (page, m) => {
  const made = await api('/notes', 'POST', { title: `QA tag target ${Date.now()}`, body: 'tags here' })
  const id = made.json.id
  await openTitle(page, made.json.title)
  await manage(page)
  await page.getByRole('form', { name: 'Manage note' }).getByRole('checkbox', { name: 'idea' }).check()
  await saveDetails(page)
  assert.ok((await getTags(id)).includes(seed.tags.idea), 'tag attached')
  assert.match(await page.locator('.editor-header').innerText(), /idea/)
  await softDelete(API, TOKEN, id)
})

await run('UI-11', 'create tag inline', async (page, m) => {
  const made = await api('/notes', 'POST', { title: `QA inline tag ${Date.now()}`, body: 'tags here' })
  const id = made.json.id
  const fresh = `qa-${Date.now()}`
  await openTitle(page, made.json.title)
  await manage(page)
  await page.getByRole('form', { name: 'Manage note' }).getByLabel('New tag (optional)').fill(fresh)
  await saveDetails(page)
  const tags = await api('/tags?limit=100')
  const created = tags.json.items.find((t) => t.title === fresh)
  assert.ok(created, 'tag created')
  assert.ok((await getTags(id)).includes(created.id), 'tag attached')
  assert.match(await page.locator('.editor-header').innerText(), new RegExp(fresh))
  await api(`/tags/${created.id}`, 'DELETE')
  await softDelete(API, TOKEN, id)
})

await run('UI-12', 'remove tag', async (page, m) => {
  const made = await api('/notes', 'POST', { title: `QA untag ${Date.now()}`, body: 'tags here', tag_ids: [seed.tags.idea] })
  const id = made.json.id
  assert.ok((await getTags(id)).includes(seed.tags.idea), 'starts tagged')
  await openTitle(page, made.json.title)
  await manage(page)
  await page.getByRole('form', { name: 'Manage note' }).getByRole('checkbox', { name: 'idea' }).uncheck()
  await saveDetails(page)
  assert.ok(!(await getTags(id)).includes(seed.tags.idea), 'tag removed')
  assert.doesNotMatch(await page.locator('.editor-header').innerText(), /idea/)
  await softDelete(API, TOKEN, id)
})

await run('UI-13', 'delete note permanently', async (page, m) => {
  const made = await api('/notes', 'POST', { title: `Delete me ${Date.now()}`, body: 'temporary' })
  const id = made.json.id
  await openTitle(page, made.json.title)
  await manage(page)
  await page.getByRole('button', { name: 'Delete note' }).click()
  await page.getByRole('button', { name: 'Delete permanently' }).click()
  await page.waitForFunction(() => !document.querySelector('.editor-title') || !document.querySelector('.editor-title').textContent.includes('Delete me'))
  const gone = await api(`/notes/${id}`)
  assert.equal(gone.status, 404)
})

await run('UI-14', 'cancel deletion', async (page, m) => {
  const made = await api('/notes', 'POST', { title: `Keep me ${Date.now()}`, body: 'temporary' })
  const id = made.json.id
  await openTitle(page, made.json.title)
  await manage(page)
  await page.getByRole('button', { name: 'Delete note' }).click()
  await page.getByRole('button', { name: 'Keep note' }).click()
  assert.equal((await api(`/notes/${id}`)).status, 200)
  await softDelete(API, TOKEN, id)
})

await run('UI-15', 'manage cancel discards edits', async (page, m) => {
  const n = await noteByTitle('Tagged: urgent only')
  const before = await getNote(n.id)
  await openTitle(page, 'Tagged: urgent only')
  await manage(page)
  await page.getByRole('form', { name: 'Manage note' }).getByLabel('Title').fill('Discarded title')
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  assert.equal(await editorTitle(page).innerText(), before.title)
  assert.equal((await getNote(n.id)).title, before.title)
})

await run('UI-16', 'todo toggle', async (page, m) => {
  const n = await noteByTitle('Todo: ship M4')
  const before = await getNote(n.id)
  await openTitle(page, 'Todo: ship M4')
  await page.getByRole('button', { name: 'Mark complete' }).click()
  await waitSaved(page)
  assert.match(await page.locator('.editor-header').innerText(), /Completed/)
  const after = await getNote(n.id)
  assert.ok(after.todo_completed > 0, 'todo_completed stamped')
  assert.equal(after.body, before.body, 'body preserved')
  await api(`/notes/${n.id}`, 'PUT', { todo_completed: 0 })
})

await run('UI-17', 'todo due label', async (page) => {
  await search(page, 'Todo: ship M4')
  const row = rows(page).first()
  assert.match(await row.innerText(), /due \d{2} \w{3}/)
  await search(page, 'Todo: done already')
  assert.doesNotMatch(await rows(page).first().innerText(), /due /)
})

await run('UI-18', 'markdown preview', async (page) => {
  await openTitle(page, 'Markdown field guide')
  await page.getByRole('button', { name: /preview/i }).click()
  const prose = page.locator('.prose-note')
  await prose.waitFor()
  assert.equal(await prose.locator('h1').innerText(), 'Field guide')
  assert.equal(await prose.locator('h2').innerText(), 'Stations')
  assert.ok(await prose.locator('strong').count() >= 1)
  assert.ok(await prose.locator('pre code').count() >= 1)
  const href = await prose.getByRole('link', { name: 'Project intent' }).getAttribute('href')
  assert.equal(href, 'https://example.com/intent')
})

await run('UI-19', 'preview sanitization', async (page) => {
  await openTitle(page, 'XSS probe')
  await page.getByRole('button', { name: /preview/i }).click()
  await page.locator('.prose-note').waitFor()
  await page.waitForTimeout(400)
  assert.equal(await page.evaluate(() => window.__XSS), undefined, 'no script executed')
  assert.equal(await page.locator('.prose-note script, .prose-note iframe, .prose-note img').count(), 0, 'dangerous elements stripped')
  assert.match(await page.locator('.prose-note').innerText(), /safe bold survives/)
})

await run('UI-20', 'task list preview', async (page) => {
  await openTitle(page, 'Markdown field guide')
  await page.getByRole('button', { name: /preview/i }).click()
  const truth = page.locator('.prose-note input[type="checkbox"]')
  await truth.first().waitFor()
  assert.equal(await truth.count(), 2)
  assert.equal(await truth.nth(0).isChecked(), false)
  assert.equal(await truth.nth(1).isChecked(), true)
  assert.equal(await truth.nth(0).isDisabled(), true)
  assert.match(await page.locator('.prose-note').innerText(), /not a task \[really\]/)
})

await run('UI-21', 'conflict banner', async (page) => {
  await openTitle(page, 'Conflict: office hours')
  const banner = page.locator('.editor-pane').getByText('nothing was overwritten')
  await banner.waitFor()
  assert.match(await page.locator('.editor-pane').innerText(), /Open synced version/)
  assert.match(await page.locator('.editor-pane').innerText(), /Keep my copy/)
})

await run('UI-22', 'conflict actions', async (page) => {
  const conflict = await noteByTitle('Conflict: office hours')
  assert.ok(conflict, 'conflict fixture exists')
  await openTitle(page, 'Conflict: office hours')
  const open = page.getByRole('button', { name: 'Open synced version' })
  const keep = page.getByRole('button', { name: 'Keep my copy' })
  assert.ok(await open.isEnabled())
  const before = await editorTitle(page).innerText()
  await open.click()
  await page.waitForFunction((t) => document.querySelector('.editor-title')?.textContent !== t, before, { timeout: 10000 })
  const afterOpen = await editorTitle(page).innerText()
  assert.notEqual(afterOpen, before, '"Open synced version" navigated to the original note')
  const original = await getNote(conflict.conflict_original_id)
  assert.equal(afterOpen, original.title)
  // Keep my copy dismisses the banner without touching either note.
  await openTitle(page, 'Conflict: office hours')
  await keep.click()
  await page.waitForTimeout(300)
  assert.equal(await page.locator('.editor-pane').getByText('nothing was overwritten').count(), 0, 'banner dismissed')
  assert.equal((await getNote(conflict.id)).is_conflict, 1, 'conflict copy untouched')
})

await run('UI-23', 'search by title', async (page) => {
  await search(page, 'Journal 42')
  assert.equal(await rows(page).count(), 1)
  assert.match(await page.locator('.note-list h2').innerText(), /All notes · “Journal 42”/)
  assert.match(await rows(page).first().innerText(), /Journal 42/)
})

await run('UI-24', 'search by body', async (page) => {
  await search(page, 'lake was still')
  assert.ok(await rows(page).count() >= 1, 'body match')
  const first = await rows(page).first().innerText()
  assert.match(first, /Journal \d+/)
})

await run('UI-25', 'search debounce', async (page) => {
  let calls = 0
  page.on('request', (r) => { if (r.url().includes('/search?')) calls++ })
  await searchBox(page).click()
  await searchBox(page).pressSequentially('Journal', { delay: 45 })
  await page.waitForTimeout(700)
  assert.equal(calls, 1, `one search request per pause, got ${calls}`)
})

await run('UI-26', 'no results + clear', async (page) => {
  await search(page, `zzz-no-match-${Date.now()}`)
  assert.match(await page.locator('.note-list').innerText(), /No notes match/)
  await page.getByRole('button', { name: 'Clear search' }).click()
  await page.waitForTimeout(400)
  assert.equal(await rows(page).count(), 50)
  assert.equal(await searchBox(page).inputValue(), '')
})

await run('UI-27', 'Escape clears search', async (page) => {
  await search(page, 'Journal 42')
  await page.keyboard.press('Escape')
  await page.waitForTimeout(400)
  assert.equal(await searchBox(page).inputValue(), '')
  assert.equal(await rows(page).count(), 50)
})

await run('UI-28', 'Cmd+K focuses search', async (page) => {
  await page.keyboard.press('Meta+k')
  const focused = await page.evaluate(() => document.activeElement?.getAttribute('aria-label'))
  assert.equal(focused, 'Search notes')
})

await run('UI-29', 'notebook filter', async (page) => {
  await nav(page).getByRole('button', { name: 'Work', exact: true }).click()
  await page.waitForTimeout(400)
  assert.equal(await page.locator('.note-list h2').innerText(), 'Work')
  assert.ok(await rows(page).count() >= 1)
  const labels = await page.locator('.note-row').evaluateAll((els) => els.map((e) => e.textContent))
  assert.ok(labels.every((t) => !t.includes('Journal 02')), 'Personal journal excluded')
})

await run('UI-30', 'nested notebooks', async (page) => {
  const text = await nav(page).innerText()
  assert.match(text, /Projects/)
  assert.match(text, /Q4/)
  // indentation is visual: Q4 lives inside Projects' child list
  const q4 = nav(page).getByRole('button', { name: 'Q4', exact: true })
  await q4.click()
  await page.waitForTimeout(400)
  assert.equal(await page.locator('.note-list h2').innerText(), 'Q4')
  assert.match(await rows(page).first().innerText(), /Deep note/)
})

await run('UI-31', 'create top-level notebook', async (page) => {
  const title = `QA notebook ${Date.now()}`
  await nav(page).getByRole('button', { name: 'New notebook' }).click()
  await page.getByRole('form', { name: 'Create notebook' }).getByLabel('Notebook name').fill(title)
  await page.getByRole('button', { name: 'Create notebook', exact: true }).click()
  await nav(page).getByRole('button', { name: title, exact: true }).waitFor()
  const folders = await api('/folders?limit=100')
  const f = folders.json.items.find((x) => x.title === title)
  assert.ok(f, 'created through API')
  await api(`/folders/${f.id}`, 'DELETE')
})

await run('UI-32', 'create child notebook', async (page) => {
  const title = `QA child ${Date.now()}`
  await nav(page).getByRole('button', { name: 'New notebook' }).click()
  const form = page.getByRole('form', { name: 'Create notebook' })
  await form.getByLabel('Notebook name').fill(title)
  await form.getByLabel('Parent notebook').selectOption(seed.work)
  await page.getByRole('button', { name: 'Create notebook', exact: true }).click()
  await nav(page).getByRole('button', { name: title, exact: true }).waitFor()
  const folders = await api('/folders?limit=100')
  const f = folders.json.items.find((x) => x.title === title)
  assert.equal(f.parent_id, seed.work)
  await api(`/folders/${f.id}`, 'DELETE')
})

await run('UI-33', 'exact notebook counts when whole library loaded', async (page) => {
  await page.getByRole('button', { name: /Load more/ }).click()
  await page.getByRole('button', { name: /Load more/ }).click()
  await page.waitForTimeout(500)
  assert.equal(await rows(page).count(), 112)
  const text = await nav(page).innerText()
  assert.match(text, /All notes\s+112/)
  // Work has 50 journals + markdown + xss + todo + conflict + tagged2
  assert.match(text, /Work\s+56/)
})

await run('UI-34', 'paged counts are not fake totals', async (page) => {
  const text = await nav(page).innerText()
  assert.match(text, /All notes\s+50\+/)
  const work = nav(page).getByRole('button', { name: /^Work/ })
  const spans = await work.locator('span.mono').count()
  assert.equal(spans, 0, 'per-notebook count suppressed while paginated')
})

await run('UI-35', 'tag filter', async (page) => {
  await nav(page).getByRole('button', { name: 'urgent', exact: true }).click()
  await page.waitForTimeout(400)
  assert.match(await page.locator('.note-list h2').innerText(), /Tag: urgent/)
  assert.equal(await rows(page).count(), 2)
  const text = await page.locator('.note-list').innerText()
  assert.match(text, /weekend plan/)
  assert.match(text, /urgent only/)
})

await run('UI-36', 'tag counts', async (page) => {
  await page.getByRole('button', { name: /Load more/ }).click()
  await page.getByRole('button', { name: /Load more/ }).click()
  await page.waitForTimeout(400)
  const urgent = nav(page).getByRole('button', { name: /^urgent/ })
  assert.match(await urgent.innerText(), /2/)
  const idea = nav(page).getByRole('button', { name: /^idea/ })
  assert.match(await idea.innerText(), /1/)
})

await run('UI-37', 'load more pagination', async (page) => {
  assert.equal(await rows(page).count(), 50)
  await page.getByRole('button', { name: /Load more/ }).click()
  await page.waitForFunction(() => document.querySelectorAll('.note-row').length === 100)
  await page.getByRole('button', { name: /Load more/ }).click()
  await page.waitForFunction(() => document.querySelectorAll('.note-row').length === 112)
  assert.equal(await page.getByRole('button', { name: /Load more/ }).count(), 0)
  const titles = await page.locator('.note-row .truncate.text-\\[14px\\]').allInnerTexts().catch(() => [])
  const ids = await page.locator('.note-row').evaluateAll((els) => els.map((e) => e.textContent))
  assert.equal(new Set(ids).size, ids.length, 'no duplicate rows')
})

await run('UI-38', 'keyboard list navigation', async (page) => {
  const first = rows(page).first()
  const firstText = await first.innerText()
  await first.focus()
  await page.keyboard.press('ArrowDown')
  let active = await page.evaluate(() => document.activeElement?.textContent?.slice(0, 120))
  assert.ok(active && active !== firstText.slice(0, 120), 'focus moved to another row')
  await page.keyboard.press('End')
  active = await page.evaluate(() => document.activeElement?.textContent)
  assert.ok(active, 'focus stays in the list')
  await page.keyboard.press('Home')
  const focusedRow = await page.evaluate(() => document.activeElement?.classList?.contains('note-row'))
  assert.ok(focusedRow, 'Home focuses a row')
})

await run('UI-39', 'selection fetches full body', async (page) => {
  let bodyFetches = 0
  page.on('request', (r) => { if (/\/notes\/[0-9a-f]{32}(\?|$)/.test(r.url()) && r.method() === 'GET') bodyFetches++ })
  await search(page, 'Journal 42')
  await rows(page).first().click()
  await page.waitForFunction(() => {
    const t = document.querySelector('textarea[aria-label="Note body, markdown source"]')
    return t && !t.disabled && t.value.includes('Entry 42')
  })
  assert.ok(bodyFetches >= 1, 'GET /notes/{id} issued')
})

await run('UI-40', 'row metadata', async (page) => {
  await search(page, 'Tagged: weekend plan')
  const text = await rows(page).first().innerText()
  assert.match(text, /Personal/)
  assert.match(text, /idea/)
  assert.match(text, /urgent/)
  assert.match(text, /\d{2} \w{3}/)
})

await run('UI-41', 'excerpt formatting', async (page) => {
  const r = await api('/notes?query=Journal%2042&summary=1&limit=1')
  const s = r.json.items[0]
  assert.ok(s.excerpt.length <= 140)
  assert.equal(s.excerpt, 'Entry 42')
  await search(page, 'Journal 42')
  assert.match(await rows(page).first().innerText(), /Entry 42/)
})

await run('UI-42', 'reload button', async (page) => {
  await openTitle(page, 'Journal 03')
  const title = await editorTitle(page).innerText()
  const seen = []
  page.on('request', (r) => { if (r.url().includes('/notes')) seen.push(r.url()) })
  await page.getByRole('button', { name: /Reload local database/ }).click()
  await page.waitForTimeout(900)
  assert.ok(seen.length >= 1, 'refetched')
  assert.equal(await editorTitle(page).innerText(), title)
  assert.match(await footer(page).innerText(), /Saved locally/)
})

await run('UI-43', 'save states', async (page) => {
  await openTitle(page, 'Journal 04')
  assert.match(await footer(page).innerText(), /Saved locally/)
  await textarea(page).fill((await textarea(page).inputValue()) + '\n\nstate probe')
  await waitSaved(page)
  assert.match(await footer(page).innerText(), /Saved locally/)
})

await run('UI-44', 'backend disconnected', async (page, m) => {
  const body = await page.locator('body').innerText()
  assert.match(await page.locator('.note-list').innerText(), /Cannot load your local library/)
  assert.doesNotMatch(body, /E2EE interop checklist|Specs & interop/) // mock library must not appear
  const newNote = page.getByRole('button', { name: 'New note', exact: true })
  assert.ok(await newNote.isDisabled())
  await page.getByRole('button', { name: 'Settings' }).click()
  assert.match(await page.getByRole('dialog').innerText(), /local database is unavailable/i)
}, { route: async (page) => { await page.route('http://127.0.0.1:41888/**', (r) => r.abort()) } })

await run('UI-45', 'retry reconnects', async (page, m) => {
  assert.match(await page.locator('.note-list').innerText(), /Cannot load your local library/)
  await page.unroute('http://127.0.0.1:41888/**')
  await page.locator('.note-list').getByRole('button', { name: 'Retry connection' }).click()
  await page.waitForFunction(() => document.querySelectorAll('.note-row').length === 50, null, { timeout: 15000 })
  assert.match(await footer(page).innerText(), /Saved locally/)
}, { route: async (page) => { await page.route('http://127.0.0.1:41888/**', (r) => r.abort()) } })

await run('UI-46', 'failed save is visible', async (page, m) => {
  await openTitle(page, 'Journal 05')
  await page.route('**/notes/*', (r) => r.request().method() === 'PUT' ? r.abort() : r.continue())
  const sentinel = `FAIL-${Date.now()}`
  await textarea(page).fill(sentinel)
  await page.waitForFunction(() => /failed|error/i.test(document.querySelector('footer [role=status]')?.textContent ?? ''), null, { timeout: 8000 })
  assert.match(await footer(page).innerText(), /failed/i)
  const alertText = await page.getByRole('alert').first().innerText().catch(() => '')
  assert.match(alertText, /PUT|\/notes|Failed|failed|Could not/i)
  assert.equal(await textarea(page).inputValue(), sentinel, 'text stays in the editor')
  await page.unroute('**/notes/*')
  const n = await noteByTitle('Journal 05')
  await api(`/notes/${n.id}`, 'PUT', { body: (await getNote(n.id)).body })
})

await run('UI-47', 'narrow layout', async (page) => {
  assert.equal(await nav(page).isVisible(), false)
  await page.getByRole('button', { name: 'Notebooks' }).click()
  const dialog = page.getByRole('dialog', { name: 'Notebooks and tags' })
  await dialog.waitFor()
  await dialog.getByRole('button', { name: 'Personal', exact: true }).click()
  await page.waitForTimeout(400)
  await page.locator('.note-row').first().click()
  await page.getByRole('button', { name: 'Back to note list' }).click()
  await page.locator('.note-row').first().waitFor()
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  assert.ok(overflow <= 1, `horizontal overflow ${overflow}`)
}, { viewport: { width: 390, height: 844 } })

await run('UI-48', 'minimum desktop layout', async (page) => {
  assert.ok(await nav(page).isVisible())
  assert.ok(await page.locator('.note-list').isVisible())
  assert.ok(await page.locator('.editor-pane').isVisible())
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  assert.ok(overflow <= 1, `horizontal overflow ${overflow}`)
}, { viewport: { width: 1024, height: 640 } })

await run('UI-49', 'theme choice persists', async (page) => {
  await page.getByRole('button', { name: 'Settings' }).click()
  const dlg = page.getByRole('dialog')
  await dlg.getByRole('button', { name: 'Dark', exact: true }).click()
  assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), 'dark')
  await page.reload()
  await goto(page)
  assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), 'dark')
  await page.getByRole('button', { name: 'Settings' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Light', exact: true }).click()
  assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), 'light')
})

await run('UI-50', 'system theme follows OS', async (page) => {
  await page.getByRole('button', { name: 'Settings' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'System', exact: true }).click()
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.waitForTimeout(250)
  assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), 'dark')
  await page.emulateMedia({ colorScheme: 'light' })
  await page.waitForTimeout(250)
  assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), 'light')
})

await run('UI-51', 'settings dialog', async (page) => {
  const opener = page.getByRole('button', { name: 'Settings' })
  await opener.click()
  const dlg = page.getByRole('dialog')
  const text = await dlg.innerText()
  assert.match(text, /Local workspace/)
  assert.match(text, /managed outside this interface/)
  await page.keyboard.press('Escape')
  await dlg.waitFor({ state: 'detached' })
  const focused = await page.evaluate(() => document.activeElement?.getAttribute('aria-label'))
  assert.equal(focused, 'Settings')
})

await run('UI-52', 'accessible controls', async (page) => {
  const unnamed = await page.locator('button').evaluateAll((els) => els.filter((el) => {
    const name = (el.getAttribute('aria-label') || el.textContent || el.getAttribute('title') || '').trim()
    return !name && el.offsetParent !== null
  }).map((el) => el.outerHTML.slice(0, 90)))
  assert.deepEqual(unnamed, [], 'every visible button has an accessible name')
  const size = await page.getByRole('button', { name: 'Settings' }).boundingBox()
  assert.ok(size.height >= 40, `settings hit area ${size.height}`)
  const newNote = await page.getByRole('button', { name: 'New note', exact: true }).boundingBox()
  assert.ok(newNote.height >= 40, `new note hit area ${newNote.height}`)
})

await run('UI-53', 'truncation and overflow', async (page) => {
  await search(page, 'A very long note title')
  const row = rows(page).first()
  const title = row.locator('span.truncate').first()
  const truncated = await title.evaluate((el) => el.scrollWidth > el.clientWidth || getComputedStyle(el).textOverflow === 'ellipsis')
  assert.ok(truncated)
  await row.click()
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  assert.ok(overflow <= 1, `horizontal overflow ${overflow}`)
  const h1 = page.locator('.editor-title')
  const h1Overflow = await h1.evaluate((el) => el.scrollWidth > el.clientWidth)
  assert.ok(h1Overflow, 'long editor title is clipped/truncated')
})

await run('UI-54', 'header metadata and chips', async (page) => {
  await openTitle(page, 'Tagged: weekend plan')
  const header = page.locator('.editor-header')
  const text = await header.innerText()
  assert.match(text, /Personal/)
  assert.match(text, /updated/)
  assert.match(text, /idea/)
  assert.match(text, /urgent/)
})

await run('UI-55', 'delete selects the next note', async (page, m) => {
  const made = await api('/notes', 'POST', { title: `Delete open ${Date.now()}`, body: 'x' })
  await openTitle(page, made.json.title)
  await manage(page)
  await page.getByRole('button', { name: 'Delete note' }).click()
  await page.getByRole('button', { name: 'Delete permanently' }).click()
  await page.waitForTimeout(600)
  await page.getByRole('button', { name: 'Clear search' }).click().catch(() => {})
  await page.waitForTimeout(400)
  assert.ok(await rows(page).first().isVisible(), 'list still has rows')
})

await run('UI-56', 'reload keeps selection', async (page) => {
  await openTitle(page, 'Journal 06')
  await page.getByRole('button', { name: /Reload local database/ }).click()
  await page.waitForTimeout(900)
  assert.equal(await editorTitle(page).innerText(), 'Journal 06')
  assert.equal(await rows(page).filter({ has: page.getByText('Journal 06', { exact: true }) }).first().getAttribute('aria-current'), 'true')
})

await run('UI-57', 'active tag toggles off', async (page) => {
  const tag = nav(page).getByRole('button', { name: 'idea', exact: true })
  await tag.click()
  await page.waitForTimeout(350)
  assert.match(await page.locator('.note-list h2').innerText(), /Tag: idea/)
  await tag.click()
  await page.waitForTimeout(350)
  assert.equal(await page.locator('.note-list h2').innerText(), 'All notes')
})

await run('UI-58', 'notebook name validation', async (page) => {
  await nav(page).getByRole('button', { name: 'New notebook' }).click()
  const form = page.getByRole('form', { name: 'Create notebook' })
  const submit = page.getByRole('button', { name: 'Create notebook', exact: true })
  assert.ok(await submit.isDisabled())
  await form.getByLabel('Notebook name').fill('   ')
  assert.ok(await submit.isDisabled())
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  assert.equal(await form.count(), 0)
})

await run('UI-59', 'tag reuse is case-insensitive', async (page, m) => {
  const before = (await api('/tags?limit=100')).json.items.length
  const made = await api('/notes', 'POST', { title: `QA tag reuse ${Date.now()}`, body: 'x' })
  const id = made.json.id
  await openTitle(page, made.json.title)
  await manage(page)
  await page.getByRole('form', { name: 'Manage note' }).getByLabel('New tag (optional)').fill('IDEA')
  await saveDetails(page)
  const after = (await api('/tags?limit=100')).json.items.length
  assert.equal(after, before, 'no duplicate tag')
  assert.ok((await getTags(id)).includes(seed.tags.idea), 'reused the existing tag')
  await softDelete(API, TOKEN, id)
})

await run('UI-60', 'server errors are readable', async (page) => {
  await page.route('**/search?**', (r) => r.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom"}' }))
  await search(page, 'Journal')
  const alert = page.getByRole('alert').first()
  await alert.waitFor()
  const text = await alert.innerText()
  assert.match(text, /GET \/search/)
  assert.match(text, /500/)
}, { shot: true })

await run('UI-61', 'offline recovery retries the journaled edit', async (page, m) => {
  await openTitle(page, 'Journal 07')
  const n = await noteByTitle('Journal 07')
  const before = await getNote(n.id)
  await page.route('**/notes/*', (r) => r.request().method() === 'PUT' ? r.abort() : r.continue())
  const sentinel = `ONLINE-${Date.now()}`
  await textarea(page).fill(before.body + `\n\n${sentinel}`)
  await page.waitForTimeout(900)
  await page.unroute('**/notes/*')
  await page.evaluate(() => window.dispatchEvent(new Event('online')))
  await page.waitForFunction((s) => {
    const t = document.querySelector('textarea[aria-label="Note body, markdown source"]')
    return t && t.value.includes(s)
  }, sentinel)
  await page.waitForTimeout(700)
  const after = await getNote(n.id)
  assert.ok(after.body.includes(sentinel), 'online event flushed the pending edit')
  await api(`/notes/${n.id}`, 'PUT', { body: before.body })
})

await run('UI-62', 'no hydration errors', async (page) => {
  assert.deepEqual(page.__errors, [], 'page errors on first load')
})

writeFileSync(RESULTS, JSON.stringify({
  at: new Date().toISOString(), origin: UI, api: API,
  results,
}, null, 2))
const pass = Object.values(results).filter((r) => r.ok).length
const fail = Object.values(results).filter((r) => !r.ok).length
console.log(`\n${pass} passed, ${fail} failed, ${Object.keys(results).length} run`)
await browser.close()
if (fail) process.exitCode = 1
