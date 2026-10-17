// Run with an isolated Vite UI and disposable API containing more than 50 notes:
// NOTE_LIST_ORIGIN=http://127.0.0.1:4331 node web/note-list.test.mjs
import assert from 'node:assert/strict'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE ?? '../.amp/in/harness/node_modules/playwright-core/index.mjs')
const origin = process.env.NOTE_LIST_ORIGIN ?? 'http://127.0.0.1:4331'
const base = process.env.NOTE_LIST_API ?? 'http://127.0.0.1:41888'
const artifacts = process.env.NOTE_LIST_ARTIFACTS
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROME_PATH ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' })
try {
  for (const width of [1440, 390]) {
    const context = await browser.newContext({ viewport: { width, height: 900 }, colorScheme: 'light' })
    await context.grantPermissions(['local-network-access'], { origin })
    await context.addInitScript(({ base, token }) => { window.__GOJOPLIN__ = { base, token } }, { base, token: process.env.NOTE_LIST_TOKEN ?? 'local-qa-disposable' })
    const page = await context.newPage()
    const errors = []
    const details = []
    page.on('pageerror', error => errors.push(error.message))
    page.on('response', response => {
      if (new RegExp(`${base}/notes/[a-f0-9]+\\?`).test(response.url()) && response.ok()) details.push(response.url())
    })
    await page.goto(origin)
    const rows = page.locator('.note-row')
    await page.waitForFunction(() => document.querySelectorAll('.note-row').length === 50)
    const selected = await page.locator('.note-row[aria-current]').innerText()
    const focused = () => rows.evaluateAll(elements => elements.indexOf(document.activeElement))
    await rows.first().focus()
    await page.keyboard.press('ArrowDown')
    assert.equal(await focused(), 1)
    assert.equal(await page.locator('.note-row[aria-current]').innerText(), selected, 'Moving focus does not activate a note')
    await page.keyboard.press('ArrowUp')
    assert.equal(await focused(), 0)
    await page.keyboard.press('ArrowUp')
    assert.equal(await focused(), 0)
    await page.keyboard.press('End')
    assert.equal(await focused(), 49)
    await page.waitForFunction(() => {
      const row = document.activeElement.getBoundingClientRect()
      const list = document.querySelector('.note-list > ul').getBoundingClientRect()
      return row.top >= list.top && row.bottom <= list.bottom + 1
    })
    await page.keyboard.press('ArrowDown')
    assert.equal(await focused(), 49)
    for (const key of ['Shift+ArrowUp', 'Alt+ArrowUp', 'Control+Home', 'Meta+Home']) {
      await page.keyboard.press(key)
      assert.equal(await focused(), 49, key)
    }
    await page.keyboard.press('Tab')
    assert.equal(await page.getByRole('button', { name: 'Load more notes' }).evaluate(el => el === document.activeElement), true, 'Tab leaves the rows')
    await page.keyboard.press('Shift+Tab')
    assert.equal(await focused(), 49)
    await page.keyboard.press('Home')
    assert.equal(await focused(), 0)
    await page.waitForFunction(() => document.querySelector('.note-list > ul').scrollTop === 0)
    assert.equal(await rows.count(), 50, 'Offscreen buttons remain in the DOM')
    assert.equal(await rows.last().getAttribute('type'), 'button')
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'No horizontal page overflow')
    if (artifacts) await page.screenshot({ path: `${artifacts}/${width}-list.png` })

    await page.getByRole('button', { name: 'Load more notes' }).click()
    await page.waitForFunction(() => document.querySelectorAll('.note-row').length === 100)
    await rows.first().focus()
    await page.keyboard.press('End')
    assert.equal(await focused(), 99)
    await page.waitForFunction(() => {
      const row = document.activeElement.getBoundingClientRect()
      const list = document.querySelector('.note-list > ul').getBoundingClientRect()
      return row.top >= list.top && row.bottom <= list.bottom + 1
    })
    const title = await rows.last().locator('.truncate').first().innerText()
    const requestsBefore = details.length
    await page.keyboard.press('Enter')
    await page.waitForFunction(title => document.querySelector('.editor-title')?.textContent === title, title)
    await page.waitForFunction(() => !document.querySelector('textarea')?.disabled)
    assert.ok(details.length > requestsBefore, 'Selection fetched a full note from the API')
    const response = await context.request.get(details.at(-1))
    const note = await response.json()
    assert.equal(note.title, title)
    assert.equal(await page.locator('textarea').inputValue(), note.body)
    if (width === 390) await page.getByRole('button', { name: 'Back to note list' }).click()
    if (artifacts) await page.screenshot({ path: `${artifacts}/${width}-loaded-end.png` })

    const search = page.getByRole('searchbox', { name: 'Search notes' })
    await search.fill(title)
    await page.waitForResponse(response => response.url().startsWith(`${base}/search?`) && response.ok())
    await page.waitForFunction(title => [...document.querySelectorAll('.note-row .truncate:first-child')].some(el => el.textContent === title), title)
    await search.press('Home')
    assert.equal(await search.evaluate(el => el === document.activeElement && el.selectionStart === 0), true, 'Input keeps native Home behavior')
    await rows.first().focus()
    await page.keyboard.press('End')
    assert.equal(await focused(), await rows.count() - 1, 'Navigation uses the filtered rows')
    if (artifacts) await page.screenshot({ path: `${artifacts}/${width}-filtered.png` })
    await search.fill('mygo-list-no-match-7dc34f')
    await page.getByText('No notes match “mygo-list-no-match-7dc34f”').waitFor()
    await page.getByRole('button', { name: 'Clear search', exact: true }).click()
    await page.waitForFunction(() => document.querySelectorAll('.note-row').length === 50)
    assert.deepEqual(errors, [])
    console.log(JSON.stringify({ width, initialRows: 50, loadedRows: 100, fullNoteTitle: note.title, fullNoteBodyLength: note.body.length, errors, passed: 'keyboard boundaries/modifiers/Tab, scroll, paging, API selection, filtering, empty recovery' }))
    await context.close()
  }
} finally {
  await browser.close()
}
