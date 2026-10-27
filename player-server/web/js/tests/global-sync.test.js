import assert from 'node:assert/strict';
import { GlobalSyncCoordinator, globalSyncPresentation as view, staleSyncMS } from '../global-sync.js';
import { API } from '../api.js';
const disk = new Map(), storage = { getItem: key => disk.get(key), setItem: (key, value) => disk.set(key, value) };
let now = 1000, pending = ['episode:a', 'progress:a'], failed = false, release, block = false;
const calls = [];
const domains = {
  pending: () => ({ ids: pending, podcasts: pending.filter(id => id.startsWith('episode:')).length }),
  podcasts: async (current, commit) => {
    calls.push('podcasts');
    if (block) await new Promise(resolve => { release = resolve; });
    if (!current()) return;
    pending = pending.filter(id => !id.startsWith('episode:'));
    await commit({ next_since: 7, epoch: 'one' });
    if (failed) throw new TypeError('offline mid-pull');
  },
  progress: async () => { calls.push('progress'); pending = pending.filter(id => !id.startsWith('progress:')); },
  queue: async () => { calls.push('queue'); },
};
const sync = new GlobalSyncCoordinator({ storage, domains, now: () => now });
assert.equal(view(sync.state).hidden, true, 'standalone has no hint');
sync.bind('server#1');
assert.match(view(sync.state).text, /Not synced yet.*2 pending.*this tab/);
await sync.syncNow();
assert.deepEqual(calls, ['podcasts', 'progress', 'queue']);
assert.equal(sync.state.lastFullSuccessMs, now);
assert.equal(view(sync.state, now - 100).text, 'Synced', 'negative age is not stale');
assert.equal(view(sync.state, now + staleSyncMS - 1).action, 'Sync now');
assert.equal(view(sync.state, now - 100).hidden, true, 'a fresh complete sync needs no attention');
assert.equal(view(sync.state, now + staleSyncMS).hidden, false, 'stale sync asks for attention');
assert.equal(view(sync.state, now + staleSyncMS).action, 'Retry');
assert.match(view(sync.state, now + staleSyncMS).text, /Last synced 2 days ago/);
failed = true; pending = ['episode:b', 'progress:b']; now += staleSyncMS;
await sync.syncNow();
assert.equal(sync.state.lastFullSuccessMs, 1000, 'partial pull never advances full success');
assert.equal(JSON.parse(disk.get('player-global-sync-v1:server#1')).cursor, 7, 'applied page survives later failure');
assert.match(view(sync.state, now).text, /Sync unavailable.*Last synced 2 days/);
const restored = new GlobalSyncCoordinator({ storage, domains }); restored.bind('server#1');
assert.equal(restored.state.lastFullSuccessMs, 1000);
failed = false; block = true;
const running = sync.syncNow(); await Promise.resolve();
sync.bind('server#2');
assert.equal(sync.state.lastFullSuccessMs, null, 'new account never inherits success');
release(); await running;
assert.equal(sync.state.account, 'server#2'); assert.equal(sync.state.lastFullSuccessMs, null);
block = false;
domains.queue = async () => { throw Object.assign(new Error('expired'), { status: 401 }); };
await sync.syncNow();
assert.equal(view(sync.state).action, 'Sign in'); assert.doesNotMatch(view(sync.state).text, /unavailable/);
domains.queue = async () => {};
domains.progress = async () => { pending = ['progress:new']; };
pending = [];
await sync.syncNow();
assert.equal(sync.state.lastFullSuccessMs, now, 'edits created during the run may stay pending');
assert.equal(sync.state.pending, 1);
domains.progress = async () => {}; // Starting pending must be acknowledged.
now++;
await sync.syncNow(); assert.equal(sync.state.lastFullSuccessMs, now - 1);
pending = ['episode:unsupported'];
domains.podcasts = async () => ({ excluded: ['episode:unsupported'] });
await sync.syncNow(); assert.equal(sync.state.lastFullSuccessMs, now, 'unsupported domain pending work does not block supported domains');
sync.bind(null); assert.equal(view(sync.state).hidden, true);
globalThis.location = { pathname: '/', search: '', hash: '', href: 'playing' };
globalThis.fetch = async () => new Response(JSON.stringify({ error: 'Expired session' }), { status: 401, headers: { 'Content-Type': 'application/json' } });
for (const request of [() => API.syncConfig(), () => API.syncPodcast({}), () => API.progressBatch([], 1)]) {
  await assert.rejects(request(), error => error.status === 401);
  assert.equal(location.href, 'playing', 'background auth failure must not navigate away from playback');
}
console.log('Global sync full/partial/account/pending and exact clock checks passed.');
