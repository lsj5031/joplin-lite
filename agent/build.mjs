import { build } from 'esbuild';
import { createHash } from 'node:crypto';
import { copyFile, mkdir, readFile, writeFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, '..');
const outDir = resolve(here, 'dist');
const embedDir = resolve(root, 'internal/assistant/bundle');
const check = process.argv.includes('--check');

if (!check) {
  await mkdir(outDir, { recursive: true });
  await mkdir(embedDir, { recursive: true });
}
const result = await build({
  entryPoints: [resolve(here, 'src/entry.ts')],
  bundle: true,
  format: 'esm',
  platform: 'browser',
  target: 'es2022',
  define: { process: 'undefined' },
  outfile: resolve(outDir, 'agent.js'),
  metafile: true,
  minify: false,
  minifySyntax: true,
  write: !check,
  logLevel: 'warning',
});

const problems = [];
for (const [input, info] of Object.entries(result.metafile.inputs)) {
  if (input.startsWith('node:') || input.includes('node_modules/.bin')) problems.push(`unexpected import ${input}`);
}
for (const output of Object.values(result.metafile.outputs)) {
  for (const imported of output.imports ?? []) {
    if (imported.path.startsWith('node:')) problems.push(`unexpected output import ${imported.path}`);
  }
}
if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}

const source = check ? Buffer.from(result.outputFiles[0].contents) : await readFile(resolve(outDir, 'agent.js'));
const hash = createHash('sha256').update(source).digest('hex');
const pkg = JSON.parse(await readFile(resolve(here, 'package.json'), 'utf8'));
const manifest = {
  apiVersion: 1,
  entry: 'agent.js',
  sha256: hash,
  bytes: source.length,
  packages: {
    '@earendil-works/pi-ai': pkg.dependencies['@earendil-works/pi-ai'],
    '@earendil-works/pi-durable': pkg.dependencies['@earendil-works/pi-durable'],
    '@earendil-works/chord': pkg.dependencies['@earendil-works/chord'],
    typebox: pkg.dependencies.typebox,
  },
};
const manifestText = `${JSON.stringify(manifest, null, 2)}\n`;
const embedded = resolve(embedDir, 'agent.js');
const embeddedManifest = resolve(embedDir, 'manifest.json');

if (check) {
  const current = await readFile(embedded, 'utf8').catch(() => '');
  const currentManifest = await readFile(embeddedManifest, 'utf8').catch(() => '');
  const dist = await readFile(resolve(outDir, 'agent.js'), 'utf8').catch(() => '');
  if (current !== source.toString('utf8') || dist !== source.toString('utf8') || currentManifest !== manifestText) {
    console.error('embedded agent bundle is stale; run npm run build');
    process.exit(1);
  }
  console.log(`bundle is current (${manifest.sha256.slice(0, 12)})`);
} else {
  await copyFile(resolve(outDir, 'agent.js'), embedded);
  await writeFile(embeddedManifest, manifestText);
  console.log(`built agent.js (${source.length} bytes, sha256 ${hash.slice(0, 12)})`);
}
