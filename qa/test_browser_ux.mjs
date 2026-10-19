// Focused browser regressions. Point only at a disposable, no-sync backend:
// UI_ORIGIN=... UI_API=... UI_TOKEN=... node qa/test_browser_ux.mjs
import assert from 'node:assert/strict'
import { mkdirSync, writeFileSync } from 'node:fs'
import { chromium } from '../.amp/in/harness/node_modules/playwright-core/index.mjs'

const { UI_ORIGIN: UI, UI_API: API, UI_TOKEN: token } = process.env
assert.ok(UI && API && token, 'Explicit disposable UI_ORIGIN, UI_API and UI_TOKEN required')
const output = process.env.UI_EVIDENCE
if (output) mkdirSync(output, { recursive: true })
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROME_PATH ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' })
const results = []
const notes = []
async function api(path, method = 'GET', body) {
  const response = await fetch(`${API}${path}${path.includes('?') ? '&' : '?'}token=${encodeURIComponent(token)}`, {
    method, headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body),
  })
  assert.ok(response.ok, `${method} ${path}: ${response.status}`)
  return response.json()
}
async function makeNote(body) {
  const note = await api('/notes', 'POST', body)
  notes.push(note.id)
  return note
}
async function shot(page, name) {
  if (output) await page.screenshot({ path: `${output}/${name}.png`, animations: 'disabled' })
}
async function open(page, title) {
  await page.getByRole('searchbox').fill(title)
  const row = page.locator('.note-row').filter({ hasText: title }).first()
  await row.waitFor()
  await row.click()
  await page.waitForFunction(title => document.querySelector('.editor-header h1')?.textContent === title, title)
  await page.getByRole('button', { name: 'Manage note', exact: true }).waitFor()
  await page.waitForFunction(() => !document.querySelector('.editor-tabs button:last-child')?.disabled)
}
try {
  const stamp = Date.now()
  const original = await makeNote({ title: `UX original ${stamp}`, body: '# Original\n\nA rendered existing note.' })
  const conflict = await makeNote({ title: `UX conflict ${stamp}`, body: 'Both versions remain.', is_conflict: 1, conflict_original_id: original.id })
  // Manage on the automatically selected row must pin it through a scoped refresh.
  const from = await api('/folders', 'POST', { title: `UX from ${stamp}` })
  const to = await api('/folders', 'POST', { title: `UX to ${stamp}` })
  const tag = await api('/tags', 'POST', { title: `UX tag ${stamp}` })
  const removed = await api('/tags', 'POST', { title: `UX remove ${stamp}` })
  const addedTitle = `UX added ${stamp}`
  const moving = await makeNote({ title: `UX moving ${stamp}`, body: 'Keep this editor open', parent_id: from.id, tag_ids: [tag.id, removed.id] })
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } })
  const page = await context.newPage()
  await page.goto(UI)
  const nav = page.locator('nav:visible')
  await nav.getByRole('button', { name: new RegExp(`^${from.title}`) }).click()
  await page.waitForFunction(title => document.querySelector('.editor-header h1')?.textContent === title, moving.title)
  await page.getByRole('button', { name: 'Manage note', exact: true }).click()
  await page.getByLabel('Notebook', { exact: true }).selectOption(to.id)
  await page.getByRole('checkbox', { name: removed.title, exact: true }).uncheck()
  await page.getByLabel('New tag (optional)').fill(addedTitle)
  await page.getByRole('button', { name: 'Save details', exact: true }).click()
  await page.getByRole('form', { name: 'Manage note' }).waitFor({ state: 'hidden' })
  assert.equal(await page.locator('.note-row').count(), 0)
  assert.equal(await page.locator('.editor-header h1').innerText(), moving.title)
  assert.equal((await api(`/notes/${moving.id}`)).parent_id, to.id)
  assert.deepEqual((await api(`/notes/${moving.id}/tags?limit=100`)).items.map(tag => tag.title).sort(), [tag.title, addedTitle].sort())
  await shot(page, 'auto-selected-move')
  await nav.getByRole('button', { name: new RegExp(`^${tag.title}`) }).click()
  await page.locator('.note-row').waitFor()
  await page.getByRole('button', { name: 'Manage note', exact: true }).click()
  await page.getByRole('checkbox', { name: tag.title, exact: true }).uncheck()
  await page.getByRole('button', { name: 'Save details', exact: true }).click()
  await page.getByRole('form', { name: 'Manage note' }).waitFor({ state: 'hidden' })
  assert.equal(await page.locator('.note-row').count(), 0)
  assert.equal(await page.locator('.editor-header h1').innerText(), moving.title)
  await shot(page, 'auto-selected-untag')
  await page.getByRole('searchbox').fill(original.title)
  await page.waitForFunction(title => document.querySelector('.editor-header h1')?.textContent === title, original.title)
  await context.close()
  for (const [width, height] of [[1440, 900], [1024, 640], [390, 844]]) {
    for (const theme of ['light', 'dark']) {
      const context = await browser.newContext({ viewport: { width, height }, colorScheme: theme })
      const page = await context.newPage()
      const suffix = `${width}-${theme}`
      await page.goto(UI)
      await open(page, original.title)
      assert.equal(await page.locator('textarea').count(), 0, 'existing notes open in Preview')
      await page.getByRole('button', { name: 'Manage note', exact: true }).click()
      const cancelledTag = `UX cancelled ${stamp}-${suffix}`
      await page.getByLabel('New tag (optional)').fill(cancelledTag)
      await page.getByRole('button', { name: 'Cancel', exact: true }).click()
      await page.getByRole('button', { name: 'Manage note', exact: true }).click()
      assert.equal(await page.getByLabel('New tag (optional)').inputValue(), '')
      await shot(page, `cancel-cleared-${suffix}`)
      await page.getByRole('button', { name: 'Save details', exact: true }).click()
      await page.getByRole('form', { name: 'Manage note' }).waitFor({ state: 'hidden' })
      assert.ok(!(await api('/tags?limit=100')).items.some(tag => tag.title === cancelledTag), 'cancelled tag must never be created')

      await open(page, conflict.title)
      const dismiss = page.getByRole('button', { name: 'Dismiss for now', exact: true })
      await dismiss.waitFor()
      const explanation = page.getByText('Another device changed this note at the same time — nothing was overwritten.')
      const textBox = await explanation.boundingBox()
      assert.ok(textBox.width >= 250, `readable explanation: ${textBox.width}`)
      if (width < 1280) assert.ok((await dismiss.boundingBox()).y >= textBox.y + textBox.height, 'actions below explanation')
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
      await shot(page, `conflict-${suffix}`)
      await dismiss.click()
      assert.equal((await api(`/notes/${conflict.id}`)).is_conflict, 1, 'dismissal preserves conflict')
      await shot(page, `dismissed-${suffix}`)

      await page.getByRole('button', { name: 'Settings', exact: true }).click()
      const focusTrace = []
      for (const key of ['Tab', 'Shift+Tab']) {
        for (let i = 0; i < 12; i++) {
          await page.keyboard.press(key)
          const focus = await page.evaluate(() => ({ tag: document.activeElement.tagName, inDialog: !!document.activeElement.closest('dialog'), label: document.activeElement.getAttribute('aria-label') }))
          assert.ok(focus.inDialog || focus.tag === 'BODY', `focus escaped into app: ${JSON.stringify(focus)}`)
          focusTrace.push(focus)
        }
      }
      await shot(page, `settings-${suffix}`)
      await page.keyboard.press('Escape')
      assert.equal(await page.getByRole('button', { name: 'Settings', exact: true }).evaluate(e => e === document.activeElement), true)

      await page.getByRole('button', { name: 'New note', exact: true }).click()
      const body = page.getByLabel('Note body, markdown source')
      await body.waitFor()
      await page.waitForFunction(() => document.activeElement?.getAttribute('aria-label') === 'Note body, markdown source')
      await shot(page, `new-write-${suffix}`)
      const createdTitle = `UX created ${stamp}-${suffix}`
      await page.getByRole('button', { name: 'Manage note', exact: true }).click()
      await page.getByLabel('Title', { exact: true }).fill(createdTitle)
      await page.getByRole('button', { name: 'Save details', exact: true }).click()
      await page.getByRole('form', { name: 'Manage note' }).waitFor({ state: 'hidden' })
      assert.equal(await body.evaluate(e => e === document.activeElement), false, 'saving details must not refocus body')
      const created = (await api(`/notes?query=${encodeURIComponent(createdTitle)}&limit=100`)).items.find(note => note.title === createdTitle)
      assert.ok(created)
      notes.push(created.id)
      if (width === 390) await page.getByRole('button', { name: 'Back to note list' }).click()
      await open(page, createdTitle)
      await page.waitForFunction(() => document.querySelector('.editor-tabs button[aria-pressed="true"]')?.textContent === 'preview')
      assert.equal(await page.locator('textarea').count(), 0, 'reopening even the same new note uses Preview')
      await open(page, original.title)
      assert.equal(await page.locator('textarea').count(), 0)
      if (width === 390) await page.getByRole('button', { name: 'Back to note list' }).click()
      const nav = page.locator('nav:visible')
      if (width === 390) await page.getByRole('button', { name: 'Notebooks', exact: true }).click()
      assert.equal(await nav.getByRole('button', { name: 'All notes', exact: true }).innerText(), 'All notes', 'scoped list must not supply library total')
      if (width === 390) await page.keyboard.press('Escape')
      if (width === 390) {
        const doomed = await makeNote({ title: `UX delete ${stamp}-${theme}`, body: 'Sole search result' })
        await open(page, doomed.title)
        await page.getByRole('button', { name: 'Manage note', exact: true }).click()
        await page.getByRole('button', { name: 'Delete note', exact: true }).click()
        await page.getByRole('button', { name: 'Delete permanently', exact: true }).click()
        await page.locator('.editor-header').waitFor({ state: 'hidden' })
        await page.getByRole('button', { name: 'Back to note list', exact: true }).waitFor()
        await shot(page, `deleted-empty-${suffix}`)
        await page.getByRole('button', { name: 'Back to note list', exact: true }).click()
        await page.getByRole('button', { name: 'Clear search', exact: true }).click()
        await page.locator('.note-row').first().waitFor()
        await shot(page, `recovered-list-${suffix}`)
        notes.splice(notes.indexOf(doomed.id), 1)
      }
      results.push({ width, theme, passed: true, focusTrace })
      await context.close()
    }
  }
} finally {
  if (output) writeFileSync(`${output}/regressions.json`, JSON.stringify(results, null, 2))
  await browser.close()
  for (const id of notes) await api(`/notes/${id}`, 'DELETE')
}
console.log(`Browser UX regressions passed across ${results.length} viewport/theme combinations`)
