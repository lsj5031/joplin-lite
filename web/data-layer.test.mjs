// Run against a Vite dev server and disposable Go API, never a personal profile.
// PLAYWRIGHT_MODULE may name an installed playwright-core module; no new dependency is required.
import assert from 'node:assert/strict'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE ?? '../.amp/in/harness/node_modules/playwright-core/index.mjs')
const origin = process.env.DATA_TEST_ORIGIN ?? 'http://127.0.0.1:4321'
const base = process.env.DATA_TEST_API ?? 'http://127.0.0.1:41888'
const token = process.env.DATA_TEST_TOKEN ?? 'local-qa-disposable'
const api = async (path, method = 'GET', body) => {
  const res = await fetch(`${base}${path}${path.includes('?') ? '&' : '?'}token=${token}`, {
    method, headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body),
  })
  assert.ok(res.ok, `${method} ${path}: ${res.status}`)
  return res.json()
}
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROME_PATH ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' })
const context = await browser.newContext()
await context.grantPermissions(['local-network-access'], { origin })
const page = await context.newPage()
const pageErrors = []
page.on('pageerror', (error) => { pageErrors.push(error.message); console.error(error) })
page.on('console', (message) => { if (message.type() === 'error') console.error(message.text()) })
const html = `<div id="probe"></div><script type="module">
import RefreshRuntime from '/@react-refresh';
RefreshRuntime.injectIntoGlobalHook(window);
window.$RefreshReg$ = () => {}; window.$RefreshSig$ = () => (type) => type;
window.__vite_plugin_react_preamble_installed__ = true;
const { mountProbe } = await import('/src/lib/vault.test.tsx');
mountProbe();
</script>`
await context.route(`${origin}/data-layer-test`, (route) => route.fulfill({ contentType: 'text/html', body: html }))
await context.addInitScript(({ base, token }) => { window.__GOJOPLIN__ = { base, token } }, { base, token })
const ready = () => page.waitForFunction(() => window.testVault?.canWrite && !window.testVault.listLoading)
const saved = () => page.waitForFunction(() => window.testVault.saveState === 'saved')
const result = (name, details) => console.log(JSON.stringify({ passed: name, ...details }))
const ids = { notes: [], folders: [], tags: [] }
try {
  await page.goto(`${origin}/data-layer-test`)
  await ready()
  ids.folders.push(await page.evaluate(() => window.testVault.createNotebook('Data contract parent')))
  await ready()
  ids.folders.push(await page.evaluate((id) => window.testVault.createNotebook('Data contract child', id), ids.folders[0]))
  await ready()
  ids.folders.push(await page.evaluate((id) => window.testVault.createNotebook('Data contract grandchild', id), ids.folders[1]))
  await ready()
  ids.tags.push(await page.evaluate(() => window.testVault.createTag('data-contract-tag')))
  await ready()
  ids.notes.push(await page.evaluate((notebookId) => window.testVault.createNote({ title: 'Data contract note', body: 'Original server body', notebookId }), ids.folders[0]))
  await ready()
  const id = ids.notes[0]
  await page.evaluate(({ id, tag }) => window.testVault.setNoteTags(id, [tag]), { id, tag: ids.tags[0] })
  await page.evaluate(({ id, folder }) => window.testVault.patchNote(id, { title: 'Renamed contract', notebookId: folder, isTodo: true, todoCompleted: true }), { id, folder: ids.folders[2] })
  assert.equal((await api(`/notes/${id}`)).parent_id, ids.folders[2])
  assert.equal((await api(`/notes/${id}`)).todo_completed, 1)
  assert.equal(await page.evaluate((id) => window.testVault.noteCountByNotebook(id), ids.folders[0]), 1)
  result('CRUD create notebook/tag/note, rename/move/todo and recursive counts')

  // Asymmetric membership: a sibling stays, an unrelated note never enters.
  const kept = await api('/notes', 'POST', { title: 'Scoped control', body: 'Control body', parent_id: ids.folders[2] })
  const outside = await api('/notes', 'POST', { title: 'Outside control', body: 'Outside body' })
  ids.notes.push(kept.id, outside.id)
  await page.evaluate((notebookId) => window.testVault.loadList({ notebookId }), ids.folders[2]); await ready()
  await page.evaluate((id) => window.testVault.selectNote(id), id)
  await page.evaluate((id) => window.testVault.patchNote(id, { notebookId: '' }), id); await ready()
  assert.deepEqual(await page.evaluate(() => window.testVault.notes.map((n) => n.id)), [kept.id])
  assert.equal(await page.evaluate(() => window.testVault.activeNote.id), id)
  assert.equal(await page.evaluate(() => window.testVault.activeNote.body), 'Original server body')
  // Continue the same details save after moving out: metadata must not reset the scope/editor.
  ids.tags.push(await page.evaluate(() => window.testVault.createTag('unrelated-contract-tag'))); await ready()
  await page.evaluate(({ id, tags }) => window.testVault.setNoteTags(id, tags), { id, tags: ids.tags })
  assert.deepEqual(await page.evaluate(() => window.testVault.notes.map((n) => n.id)), [kept.id])
  assert.equal(await page.evaluate(() => window.testVault.activeNote.id), id)
  await page.evaluate(({ id, notebookId }) => window.testVault.patchNote(id, { notebookId }), { id, notebookId: ids.folders[2] }); await ready()
  assert.deepEqual(new Set(await page.evaluate(() => window.testVault.notes.map((n) => n.id))), new Set([id, kept.id]))
  result('notebook move reconciles removal/re-entry; compound tag creation keeps scope/editor')

  await page.evaluate(({ id, tags }) => window.testVault.setNoteTags(id, tags), { id: kept.id, tags: ids.tags })
  await page.evaluate(({ id, tag }) => window.testVault.setNoteTags(id, [tag]), { id: outside.id, tag: ids.tags[1] })
  await page.evaluate((tagId) => window.testVault.loadList({ tagId }), ids.tags[0]); await ready()
  await page.evaluate((id) => window.testVault.patchNote(id, { body: 'UNSAVED BODY DURING TAG REMOVAL' }), id)
  await page.evaluate(({ id, tag }) => window.testVault.setNoteTags(id, [tag]), { id, tag: ids.tags[1] }); await ready()
  assert.deepEqual(await page.evaluate(() => window.testVault.notes.map((n) => n.id)), [kept.id])
  assert.equal(await page.evaluate(() => window.testVault.activeNote.body), 'UNSAVED BODY DURING TAG REMOVAL')
  await saved()
  assert.equal((await api(`/notes/${id}`)).body, 'UNSAVED BODY DURING TAG REMOVAL')
  assert.deepEqual((await api(`/notes/${id}/tags`)).items.map((t) => t.id), [ids.tags[1]])
  await page.evaluate(({ id, tags }) => window.testVault.setNoteTags(id, tags), { id, tags: ids.tags }); await ready()
  assert.deepEqual(new Set(await page.evaluate(() => window.testVault.notes.map((n) => n.id))), new Set([id, kept.id]))
  result('tag removal/addition reconciles scope, retains unrelated tags and unsaved body')
  await page.evaluate((id) => window.testVault.setNoteTags(id, []), kept.id)
  await page.evaluate(({ id, tag }) => window.testVault.setNoteTags(id, [tag]), { id, tag: ids.tags[0] })
  await page.evaluate(() => window.testVault.loadList({})); await ready()

  let listGets = 0
  const countLists = (req) => { if (req.method() === 'GET' && req.url().includes('summary=1')) listGets++ }
  page.on('request', countLists)
  let puts = 0
  const countPut = (req) => { if (req.method() === 'PUT' && req.url().includes(`/notes/${id}?`)) puts++ }
  page.on('request', countPut)
  await page.evaluate(async (id) => {
    for (const body of ['# New', '# New excerpt', '# New excerpt 中文 🚶\nSecond line']) await window.testVault.patchNote(id, { body })
  }, id)
  await saved()
  assert.equal(puts, 1)
  const row = await page.evaluate((id) => window.testVault.notes.find((n) => n.id === id), id)
  assert.equal(row.excerpt, 'New excerpt 中文 🚶')
  assert.deepEqual(row.tagIds, [ids.tags[0]])
  assert.deepEqual((await api(`/notes/${id}/tags`)).items.map((t) => t.id), [ids.tags[0]])
  assert.equal(listGets, 0, 'Ordinary body saves do not reload the list')
  page.off('request', countLists)
  result('debounced body save retains tags and updates excerpt', { puts, excerpt: row.excerpt })

  // Reload before the 600ms debounce expires, with writes held across the navigation.
  let blockPut = true
  const block = (route) => route.request().method() === 'PUT' && blockPut ? route.abort('connectionrefused') : route.continue()
  await page.route(`${base}/**`, block)
  await page.evaluate((id) => window.testVault.patchNote(id, { body: 'IMMEDIATE RELOAD RECOVERED' }), id)
  await page.reload()
  await page.waitForFunction(() => window.testVault?.saveState === 'error')
  assert.equal((await api(`/notes/${id}`)).body, '# New excerpt 中文 🚶\nSecond line')
  assert.equal(await page.evaluate(() => window.testVault.source), 'error')
  assert.equal(await page.evaluate(() => window.testVault.canWrite), false)
  blockPut = false
  await page.evaluate(() => window.testVault.reload())
  await ready(); await saved()
  assert.equal((await api(`/notes/${id}`)).body, 'IMMEDIATE RELOAD RECOVERED')
  result('immediate document reload recovery and failed-save retry')

  await page.route(`${base}/**`, (route) => route.abort('connectionrefused'))
  await page.reload()
  await page.waitForFunction(() => window.testVault?.source === 'error')
  assert.deepEqual(await page.evaluate(() => window.testVault.notes), [])
  assert.equal(await page.evaluate(() => window.testVault.canWrite), false)
  assert.equal(await page.evaluate(async () => { try { await window.testVault.createNote(); return false } catch { return true } }), true)
  await page.unroute(`${base}/**`)
  await page.evaluate(() => window.testVault.reload()); await ready()
  result('configured API failure exposes no mock data and rejects creation; retry recovers')

  let releaseGet, capturedGet
  const heldGet = new Promise((resolve) => { capturedGet = resolve })
  await page.route(`${base}/notes/${id}?*`, async (route) => {
    if (route.request().method() !== 'GET') return route.continue()
    const response = await route.fetch()
    capturedGet()
    await new Promise((resolve) => { releaseGet = resolve })
    await route.fulfill({ response })
  })
  await page.evaluate((id) => { void window.testVault.selectNote(id) }, id)
  await heldGet
  await page.evaluate((id) => window.testVault.patchNote(id, { body: 'USER EDIT WINS LATE GET' }), id)
  await saved()
  releaseGet()
  await page.waitForFunction(() => !window.testVault.activeLoading)
  assert.equal(await page.evaluate(() => window.testVault.activeNote.body), 'USER EDIT WINS LATE GET')
  assert.equal((await api(`/notes/${id}`)).body, 'USER EDIT WINS LATE GET')
  await page.unroute(`${base}/notes/${id}?*`)
  result('late captured GET cannot replace edits even after PUT succeeds')

  // A newer edit must survive an older in-flight PUT and be sent afterwards.
  let releasePut, capturedPut
  const heldPut = new Promise((resolve) => { capturedPut = resolve })
  let first = true
  await page.route(`${base}/notes/${id}?*`, async (route) => {
    if (route.request().method() !== 'PUT' || !first) return route.continue()
    first = false
    const response = await route.fetch()
    capturedPut()
    await new Promise((resolve) => { releasePut = resolve })
    await route.fulfill({ response })
  })
  await page.evaluate((id) => window.testVault.patchNote(id, { body: 'OLDER IN FLIGHT' }), id)
  await heldPut
  await page.evaluate((id) => window.testVault.patchNote(id, { body: 'NEWER IN FLIGHT SURVIVES' }), id)
  releasePut(); await saved()
  assert.equal((await api(`/notes/${id}`)).body, 'NEWER IN FLIGHT SURVIVES')
  assert.equal(await page.evaluate(() => window.testVault.activeNote.body), 'NEWER IN FLIGHT SURVIVES')
  await page.unroute(`${base}/notes/${id}?*`)
  result('serialized PUT responses preserve newer edits')

  await page.route(`${base}/notes/${id}?*`, (route) => route.request().method() === 'PUT' ? route.fulfill({ status: 500, body: '{}' }) : route.continue())
  assert.equal(await page.evaluate(async (id) => { try { await window.testVault.patchNote(id, { title: 'RETRY FAILED RENAME' }); return false } catch { return true } }, id), true)
  assert.equal(await page.evaluate(() => window.testVault.saveState), 'error')
  assert.equal((await api(`/notes/${id}`)).title, 'Renamed contract')
  await page.unroute(`${base}/notes/${id}?*`)
  await page.evaluate(() => window.testVault.reload()); await ready(); await saved()
  assert.equal((await api(`/notes/${id}`)).title, 'RETRY FAILED RENAME')
  for (const method of ['createNote', 'createNotebook', 'createTag']) {
    const before = await page.evaluate(() => ({ notes: window.testVault.notes.length, notebooks: window.testVault.notebooks.length, tags: window.testVault.tags.length }))
    await page.route(`${base}/**`, (route) => route.request().method() === 'POST' ? route.fulfill({ status: 403, body: '{}' }) : route.continue())
    assert.equal(await page.evaluate(async (method) => { try { await window.testVault[method]('Forbidden fixture'); return false } catch { return true } }, method), true)
    assert.deepEqual(await page.evaluate(() => ({ notes: window.testVault.notes.length, notebooks: window.testVault.notebooks.length, tags: window.testVault.tags.length })), before)
    await page.unroute(`${base}/**`)
    await page.evaluate(() => window.testVault.reload()); await ready()
  }
  result('failed rename retained/retried; failed note/notebook/tag creation rejects without phantom rows')

  await page.evaluate((id) => window.testVault.setNoteTags(id, []), id)
  assert.deepEqual((await api(`/notes/${id}/tags`)).items, [])
  await page.route(`${base}/notes/${id}?*`, (route) => route.request().method() === 'DELETE' ? route.fulfill({ status: 403, json: { error: 'folder is read-only fixture' } }) : route.continue())
  assert.match(await page.evaluate(async (id) => { try { await window.testVault.deleteNote(id); return '' } catch (error) { return error.message } }, id), /403: folder is read-only fixture/)
  assert.ok(await page.evaluate((id) => window.testVault.notes.some((n) => n.id === id), id))
  await page.unroute(`${base}/notes/${id}?*`)
  await page.evaluate(() => window.testVault.reload()); await ready()
  await page.evaluate((id) => window.testVault.deleteNote(id), id)
  assert.equal((await fetch(`${base}/notes/${id}?token=${token}`)).status, 404)
  assert.equal(await page.evaluate(() => window.testVault.activeNote), null)
  result('tag removal, failed delete rejection/state retention, successful delete')

  const demoContext = await browser.newContext()
  const demoPage = await demoContext.newPage()
  await demoContext.route(`${origin}/data-layer-test`, (route) => route.fulfill({ contentType: 'text/html', body: html }))
  await demoPage.goto(`${origin}/data-layer-test`)
  await demoPage.waitForFunction(() => window.testVault?.source === 'mock' && window.testVault.canWrite)
  const demoId = await demoPage.evaluate(() => window.testVault.createNote({ title: 'Intentional demo edit' }))
  await demoPage.evaluate((id) => window.testVault.patchNote(id, { body: 'DEMO EDIT' }), demoId)
  await demoPage.evaluate(() => window.testVault.loadList({ query: 'DEMO EDIT' }))
  assert.equal(await demoPage.evaluate(() => window.testVault.notes[0].body), 'DEMO EDIT')
  const demoFolder = await demoPage.evaluate(() => window.testVault.createNotebook('Fresh demo parent'))
  const demoChild = await demoPage.evaluate((id) => window.testVault.createNotebook('Fresh demo child', id), demoFolder)
  await demoPage.evaluate(({ id, notebookId }) => window.testVault.patchNote(id, { notebookId }), { id: demoId, notebookId: demoChild })
  await demoPage.evaluate((notebookId) => window.testVault.loadList({ notebookId }), demoFolder)
  assert.deepEqual(await demoPage.evaluate(() => window.testVault.notes.map((n) => n.id)), [demoId])
  const demoTag = await demoPage.evaluate(() => window.testVault.createTag('freshdemometadata'))
  await demoPage.evaluate(({ id, tag }) => window.testVault.setNoteTags(id, [tag]), { id: demoId, tag: demoTag })
  await demoPage.evaluate(() => window.testVault.loadList({ query: 'freshdemometadata' }))
  assert.deepEqual(await demoPage.evaluate(() => window.testVault.notes.map((n) => n.id)), [demoId])
  await demoContext.close()
  result('unconfigured demo remains editable and filters with newly created notebook/tag metadata')

  const profileContext = await browser.newContext()
  await profileContext.route(url => url.pathname === '/profile-key-test', route => route.fulfill({ contentType: 'text/html', body: `<script type="module">
    const params = new URLSearchParams(location.search);
    window.__GOJOPLIN__ = { base: params.get('base'), profile: params.get('profile') };
    const { recoveryKey } = await import('/src/lib/api.ts');
    window.testRecoveryKey = recoveryKey;
  </script>` }))
  const profilePage = await profileContext.newPage()
  for (const [profile, port, expected] of [['profile-a', 41888, null], ['profile-b', 41888, null], ['profile-a', 41999, 'draft-a']]) {
    await profilePage.goto(`${origin}/profile-key-test?profile=${profile}&base=http://127.0.0.1:${port}`)
    await profilePage.waitForFunction(() => window.testRecoveryKey)
    assert.equal(await profilePage.evaluate(() => localStorage.getItem(window.testRecoveryKey)), expected)
    if (expected === null && profile === 'profile-a') await profilePage.evaluate(() => localStorage.setItem(window.testRecoveryKey, 'draft-a'))
  }
  await profileContext.close()
  result('native recovery isolates database profiles and survives API port changes')

  await page.evaluate(async () => {
    const { recoveryKey } = await import('/src/lib/api.ts')
    localStorage.setItem(recoveryKey, 'invalid journal fixture')
  })
  await page.reload()
  await page.waitForFunction(() => window.testVault?.saveState === 'error')
  await page.evaluate(() => window.testVault.reload())
  assert.equal(await page.evaluate(() => window.testVault.canWrite), false)
  assert.equal(await page.evaluate(() => window.testVault.source), 'error')
  assert.equal(await page.evaluate(async () => {
    const { recoveryKey } = await import('/src/lib/api.ts')
    return localStorage.getItem(recoveryKey)
  }), 'invalid journal fixture')
  result('invalid recovery journal remains intact and blocks writes through retry')
  assert.deepEqual(pageErrors, [], 'No uncaught browser errors')
} finally {
  for (const id of ids.notes) await api(`/notes/${id}`, 'DELETE')
  for (const id of ids.tags) await api(`/tags/${id}`, 'DELETE')
  for (const id of ids.folders.reverse()) await api(`/folders/${id}`, 'DELETE')
  await browser.close()
}
