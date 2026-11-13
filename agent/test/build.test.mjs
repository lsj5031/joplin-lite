import assert from 'node:assert/strict';
import { test } from 'node:test';
import { cp, mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';

test('bundle check compiles source, detects source-only changes, and writes nothing', async () => {
  const agent = resolve(dirname(fileURLToPath(import.meta.url)), '..');
  const root = await mkdtemp(resolve(tmpdir(), 'ask-bundle-check-'));
  try {
    const copy = resolve(root, 'agent');
    await mkdir(copy);
    for (const path of ['build.mjs', 'package.json', 'src', 'dist']) await cp(resolve(agent, path), resolve(copy, path), { recursive: true });
    await cp(resolve(agent, '../internal/assistant/bundle'), resolve(root, 'internal/assistant/bundle'), { recursive: true });
    await cp(resolve(agent, 'node_modules'), resolve(copy, 'node_modules'), { recursive: true });
    const run = () => spawnSync(process.execPath, [resolve(copy, 'build.mjs'), '--check'], { encoding: 'utf8', cwd: copy });
    const current = run();
    assert.equal(current.status, 0, current.stderr);
    const entry = resolve(copy, 'src/entry.ts');
    await writeFile(entry, `${await readFile(entry, 'utf8')}\nglobalThis.__sourceOnlyRegression = true;\n`);
    const paths = ['agent/dist/agent.js', 'internal/assistant/bundle/agent.js', 'internal/assistant/bundle/manifest.json', 'agent/src/entry.ts'];
    const before = await Promise.all(paths.map((path) => readFile(resolve(root, path))));
    const checked = run();
    assert.equal(checked.status, 1, checked.stderr);
    assert.match(checked.stderr, /bundle is stale/);
    for (let i = 0; i < paths.length; i++) assert.deepEqual(await readFile(resolve(root, paths[i])), before[i]);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
