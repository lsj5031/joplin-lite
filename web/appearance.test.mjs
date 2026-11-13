// Run against a running UI: UI_TEST_ORIGIN=http://127.0.0.1:4318 node web/appearance.test.mjs
import assert from 'node:assert/strict'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE ?? '../.amp/in/harness/node_modules/playwright-core/index.mjs')
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROME_PATH ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' })
const origin = process.env.UI_TEST_ORIGIN ?? 'http://127.0.0.1:4318'
const errors = []
try {
  const page = await browser.newPage({ colorScheme: 'light' })
  page.on('pageerror', error => errors.push(error.message))
  const settings = () => page.getByRole('dialog', { name: 'Settings', exact: true })
  const background = () => page.locator('body').evaluate(el => getComputedStyle(el).backgroundColor)
  const open = () => page.getByRole('button', { name: 'Settings', exact: true }).click()
  const choose = name => settings().getByRole('button', { name, exact: true }).click()
  await page.goto(origin)
  await open()
  await choose('Light')
  const light = await background()
  await choose('Dark')
  const dark = await background()
  assert.notEqual(dark, light)
  await page.reload()
  await open()
  assert.equal(await settings().getByRole('button', { name: 'Dark', exact: true }).getAttribute('aria-pressed'), 'true')
  assert.equal(await background(), dark)
  await choose('System')
  assert.equal(await background(), light)
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.waitForFunction(color => getComputedStyle(document.body).backgroundColor === color, dark)
  await choose('Light')
  assert.equal(await background(), light, 'An explicit choice overrides a dark OS preference')
  await page.emulateMedia({ colorScheme: 'light' })
  await page.emulateMedia({ colorScheme: 'dark' })
  assert.equal(await background(), light)
  await page.keyboard.press('Escape')
  assert.equal(await page.getByRole('button', { name: 'Settings', exact: true }).evaluate(el => el === document.activeElement), true)

  const restricted = await browser.newPage({ colorScheme: 'light' })
  restricted.on('pageerror', error => errors.push(error.message))
  await restricted.addInitScript(() => {
    const original = Storage.prototype.setItem
    Storage.prototype.setItem = function(key, value) {
      if (key === 'joplin-lite:theme') throw new DOMException('Storage unavailable', 'QuotaExceededError')
      return original.call(this, key, value)
    }
  })
  await restricted.goto(origin)
  await restricted.getByRole('button', { name: 'Settings', exact: true }).click()
  await restricted.getByRole('dialog').getByRole('button', { name: 'Dark', exact: true }).click()
  assert.equal(await restricted.locator('body').evaluate(el => getComputedStyle(el).backgroundColor), dark, 'Storage failure still permits a session theme')

  for (const [platform, badge, shortcut] of [['Win32', 'Ctrl+K', 'Control+k'], ['Linux x86_64', 'Ctrl+K', 'Control+k'], ['MacIntel', '⌘K', 'Meta+k']]) {
    const platformPage = await browser.newPage({ viewport: { width: 1280, height: 800 } })
    platformPage.on('pageerror', error => errors.push(error.message))
    await platformPage.addInitScript(platform => Object.defineProperty(navigator, 'platform', { get: () => platform }), platform)
    await platformPage.goto(origin)
    const search = platformPage.getByRole('searchbox', { name: 'Search notes' })
    await platformPage.locator('kbd', { hasText: badge }).waitFor({ state: 'visible' })
    assert.equal(await platformPage.locator('kbd').innerText(), badge, platform)
    await platformPage.getByRole('button', { name: 'New note', exact: true }).focus()
    await platformPage.keyboard.press(shortcut)
    assert.equal(await search.evaluate(el => el === document.activeElement), true, `${platform} search shortcut`)
    await search.fill('disposable search')
    await platformPage.keyboard.press('Escape')
    assert.equal(await search.inputValue(), '')
    await platformPage.getByRole('button', { name: 'Settings', exact: true }).click()
    await platformPage.keyboard.press(shortcut)
    assert.equal(await platformPage.getByRole('dialog', { name: 'Settings', exact: true }).evaluate(el => el.contains(document.activeElement)), true, 'Search shortcut must preserve modal focus')
    await platformPage.close()
  }
  assert.deepEqual(errors, [])
  console.log('PASS appearance: themes, storage-unavailable choice, Windows/Linux/Mac shortcut labels and focus, Escape clearing and modal focus')
} finally {
  await browser.close()
}
