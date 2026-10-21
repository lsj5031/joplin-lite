import assert from 'node:assert/strict'
import { copyFile, mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { get } from 'node:http'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'
import test from 'node:test'

test('serve captures outside a checkout whose path contains spaces', async (t) => {
  const dir = await mkdtemp(join(tmpdir(), 'joplin promo '))
  t.after(() => rm(dir, { recursive: true, force: true }))
  const root = join(dir, 'source checkout')
  const promo = join(dir, 'external frames')
  const script = join(root, 'qa', 'promo', 'render.mjs')
  await mkdir(join(root, 'qa', 'promo'), { recursive: true })
  await mkdir(join(root, 'tmp', 'promo'), { recursive: true })
  await mkdir(promo)
  await copyFile(new URL('./render.mjs', import.meta.url), script)
  await writeFile(join(root, 'qa', 'promo', 'compose.html'), 'compositor')
  await writeFile(join(root, 'tmp', 'promo', 'promo.json'), 'stale manifest')
  await writeFile(join(promo, 'promo.json'), 'external manifest')
  await writeFile(join(promo, 'frame 001.png'), 'external frame')
  await writeFile(join(dir, 'private.txt'), 'outside roots')

  const old = process.env.PROMO_DIR
  process.env.PROMO_DIR = promo
  t.after(() => {
    if (old === undefined) delete process.env.PROMO_DIR
    else process.env.PROMO_DIR = old
  })
  // Importing is side-effect-free: no browser or encoder is needed to test
  // the same HTTP handler the compositor uses.
  const { serve } = await import(pathToFileURL(script).href)
  const server = await serve()
  t.after(() => new Promise((resolve, reject) => server.close(err => err ? reject(err) : resolve())))
  const request = (path) => new Promise((resolve, reject) => {
    get({ hostname: '127.0.0.1', port: server.address().port, path }, (res) => {
      let body = ''
      res.setEncoding('utf8')
      res.on('data', chunk => { body += chunk })
      res.on('end', () => resolve({ status: res.statusCode, body }))
      res.on('error', reject)
    }).on('error', reject)
  })
  assert.deepEqual(await request('/qa/promo/compose.html'), { status: 200, body: 'compositor' })
  assert.deepEqual(await request('/__promo/promo.json'), { status: 200, body: 'external manifest' })
  assert.deepEqual(await request('/__promo/frame%20001.png'), { status: 200, body: 'external frame' })
  assert.equal((await request('/__promo/../private.txt')).status, 404)
  assert.equal((await request('/__promo/..%5cprivate.txt')).status, 404)
  assert.equal((await request('/../private.txt')).status, 404)
  assert.equal((await request('/__promo/%zz')).status, 400)
})
