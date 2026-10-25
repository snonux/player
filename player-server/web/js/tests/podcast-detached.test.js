import assert from 'node:assert/strict';
import { API } from '../api.js';
import { setPodcastAccount, podcastSyncStatus, saveEpisodePosition } from '../podcast-episode-state.js';
import { prepareDetachedPlayback, transferPodcastMedia } from '../podcast-detached.js';

const realNow = Date.now, realTimeout = globalThis.setTimeout, realClear = globalThis.clearTimeout;
let now = 0, id = 0;
const timers = new Map();
Date.now = () => now;
globalThis.setTimeout = (callback, delay) => { const key = ++id; timers.set(key, { callback, due: now + delay }); return key; };
globalThis.clearTimeout = key => timers.delete(key);
const tick = () => new Promise(resolve => setImmediate(resolve));
async function advance(time) {
  now = time;
  for (const [key, timer] of [...timers]) if (timer.due <= now) { timers.delete(key); timer.callback(); }
  await tick();
}
const deferred = () => { let resolve; const promise = new Promise(done => { resolve = done; }); return { promise, resolve }; };
const ep = { feed_key: 'feed:detached.example/rss', episode_key: 'guid:1', position: { seconds: 45, observed_ms: 100 } };
const payload = { media: { id: 1, type: 'audio', podcast_episode: ep, podcast_account: 7, podcast_lease: 999 }, resumeFrom: 45 };
try {
  setPodcastAccount(null);
  const config = deferred();
  let accountApplications = 0;
  const startup = prepareDetachedPlayback(payload, { accountReady: config.promise, onAccount: cfg => { accountApplications++; setPodcastAccount(cfg.user_id); } });
  await advance(3000);
  const fallback = await startup;
  assert.equal(fallback.position, 45);
  assert.equal(fallback.authenticated, false, 'hung config uses bounded local snapshot');
  assert.equal(fallback.media.podcast_lease, null);
  config.resolve({ user_id: 7 }); await tick();
  assert.equal(accountApplications, 0, 'late config does not authorize an already-started fallback');
  setPodcastAccount(7);
  assert.equal(saveEpisodePosition(ep, 88, { lease: fallback.media.podcast_lease }), null, 'fallback cannot inherit later account writes');

  setPodcastAccount(null); now = 10000;
  const normalConfig = deferred();
  API.mediaDetail = () => new Promise(() => {});
  const normal = prepareDetachedPlayback(payload, { accountReady: normalConfig.promise, onAccount: cfg => setPodcastAccount(cfg.user_id) });
  await advance(11000); normalConfig.resolve({ user_id: 7 }); await tick();
  assert([...timers.values()].every(timer => timer.due <= 13000), 'reconciliation receives remaining budget, not another three seconds');
  await advance(13000);
  const ready = await normal;
  assert.equal(ready.authenticated, true);
  assert.equal(ready.media.podcast_lease, podcastSyncStatus().generation, 'popup reacquires its own lease');
  assert.equal(ready.media.podcast_account, 7);
  assert.notEqual(ready.media.podcast_lease, payload.media.podcast_lease);

  API.mediaDetail = async () => ({ podcast_episode: ep });
  API.syncPodcast = async () => ({ acked: [], rejected: [], episodes: [ep] });
  const reconciled = await prepareDetachedPlayback(payload, { accountReady: Promise.resolve({ user_id: 7 }), onAccount: cfg => setPodcastAccount(cfg.user_id) });
  assert.equal(reconciled.position, 45, 'normal config/reconciliation uses stable registers');
  const transferred = transferPodcastMedia(reconciled.media);
  assert.equal(transferred.podcast_lease, undefined, 'return strips popup-local lease');
  assert.equal(transferred.podcast_account, 7, 'return retains actual account identity');
  setPodcastAccount(8);
  assert.equal(transferPodcastMedia(reconciled.media), null, 'wrong-account return cannot reacquire local ownership');
  assert.equal((await prepareDetachedPlayback(payload, { accountReady: Promise.resolve({ user_id: 8 }) })).cancelled, true);

  const lateConfig = deferred();
  const switched = prepareDetachedPlayback(payload, { accountReady: lateConfig.promise });
  setPodcastAccount(9); lateConfig.resolve({ user_id: 7 }); await tick();
  assert.equal((await switched).cancelled, true, 'account change during discovery rejects old request');
  setPodcastAccount(7);
  const signedOut = await prepareDetachedPlayback(payload, { accountReady: Promise.resolve({ user_id: 0 }), onAccount: cfg => setPodcastAccount(cfg.user_id) });
  assert.equal(signedOut.authenticated, false);
  assert.equal(podcastSyncStatus().account, null, 'authoritative signout clears known account');
  assert.equal(signedOut.media.podcast_lease, null, 'signed-out fallback cannot write account state');
} finally {
  Date.now = realNow; globalThis.setTimeout = realTimeout; globalThis.clearTimeout = realClear;
  setPodcastAccount(null);
}
console.log('detached podcast deadline/account tests passed');
