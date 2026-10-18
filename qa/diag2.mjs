import { chromium } from '../.amp/in/harness/node_modules/playwright-core/index.mjs'
import { writeFileSync } from 'node:fs'
const browser = await chromium.launch({ headless: true, executablePath: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' })
const nojs = await browser.newContext({ javaScriptEnabled: false })
const a = await nojs.newPage()
await a.goto('http://127.0.0.1:4331/')
writeFileSync('/tmp/qa-prerender.html', await a.content())
await nojs.close()
const page = await browser.newPage()
page.on('pageerror', e => console.log('pageerror', e.message))
await page.goto('http://127.0.0.1:4331/')
await page.waitForTimeout(1200)
writeFileSync('/tmp/qa-hydrated.html', await page.content())
await browser.close()
