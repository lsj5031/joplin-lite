import { chromium } from '../.amp/in/harness/node_modules/playwright-core/index.mjs'
const browser = await chromium.launch({ headless: true, executablePath: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' })
const page = await browser.newPage()
page.on('console', m => console.log('console', m.type(), m.text()))
page.on('pageerror', e => console.log('pageerror', e.message))
await page.goto(process.env.ORIGIN || 'http://127.0.0.1:4331/')
await page.waitForTimeout(1500)
console.log('body text:', (await page.locator('body').innerText()).slice(0, 400))
await browser.close()
