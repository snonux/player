import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { runInNewContext } from 'node:vm';

// Observe the actual install manifest without running app modules or a browser.
const webRoot = new URL('../../', import.meta.url);
const events = {};
let installed;
runInNewContext(await readFile(new URL('sw.js', webRoot), 'utf8'), {
  self: { addEventListener: (type, listener) => { events[type] = listener; }, skipWaiting: async () => {} },
  caches: { open: async () => ({ addAll: async assets => { installed = Array.from(assets); } }) },
});
let installation;
events.install({ waitUntil: promise => { installation = promise; } });
await installation;
const cached = new Set(installed);
assert.equal(cached.size, installed.length, 'Cache.addAll rejects duplicate URLs: the install manifest must be unique');
assert(cached.has('/js/app.js'), 'the app entry point must be precached');

// Recursively follow production static imports, re-exports, side-effect imports
// and literal dynamic imports. Existing precached entry points such as detach
// must be complete too. All app.js imports are required even when a branch of
// startup only opens device files or standalone podcasts.
const visited = new Set();
async function visit(module) {
  if (visited.has(module)) return;
  visited.add(module);
  const source = await readFile(new URL(`.${module}`, webRoot), 'utf8');
  const imports = [
    /\bfrom\s*['"]([./][^'"]+)['"]/g,
    /\bimport\s*['"]([./][^'"]+)['"]/g,
    /\bimport\s*\(\s*['"]([./][^'"]+)['"]\s*\)/g,
  ];
  for (const pattern of imports) {
    for (const match of source.matchAll(pattern)) {
      const target = new URL(match[1], `https://player.test${module}`).pathname;
      assert(!target.includes('/tests/') && !target.includes('/node_modules/'), `production import must not pull test/vendor trees into the app cache: ${module} → ${target}`);
      await visit(target);
    }
  }
}
await visit('/js/app.js');
for (const asset of cached) {
  if (asset.endsWith('.js')) await visit(asset);
}
const missing = [...visited].filter(module => !cached.has(module)).sort();
assert.deepEqual(missing, [], `offline startup requires every production module in the install cache:\n${missing.join('\n')}`);
console.log(`service worker precaches the complete production import graph (${visited.size} modules)`);
