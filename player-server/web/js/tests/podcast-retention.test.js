import assert from 'node:assert/strict';
import { webcrypto } from 'node:crypto';
Object.defineProperty(globalThis, 'crypto', { value: webcrypto, configurable: true });
const saved = new Map();
globalThis.localStorage = { getItem: k => saved.get(k) ?? null, setItem: (k,v) => saved.set(k,v), removeItem: k => saved.delete(k), get length() { return saved.size; }, key: n => [...saved.keys()][n] ?? null };
const { observePodcastRetention: observe, podcastRetentionExpired: expired, podcastRetentionMS: week, readPodcastRetention: read } = await import('../podcast-retention.js');
const { localPodcastSnapshot, editLocalPodcast } = await import('../local-podcast-state.js');
const ep = { feed_key: 'feed:retention', episode_key: 'one' };
const base = 1800000000000;
let marker = observe(1, { ...ep, completion: { completed: true, observed_ms: base - week * 3 } }, base);
assert.equal(expired(marker, base + week - 1), false);
assert.equal(expired(marker, base + week), true);
marker = observe(1, { ...ep, completion: { completed: true, observed_ms: base - week * 3 } }, base + week - 1);
assert.equal(marker.seen_ms, base, 'repeated completion never extends grace');
assert.deepEqual(read(2), [], 'accounts are separate');
marker = observe(1, { ...ep, position: { observed_ms: base } }, base + week);
assert.equal(expired(marker, base + week), false, 'new playback protects completed entry');
marker = observe(1, { ...ep, completion: { completed: true, observed_ms: base + week } }, base + week);
assert.equal(expired(marker, base + week * 2), true, 'recompletion has fresh grace');
marker = observe(1, { ...ep, deletion: { deleted: false, observed_ms: base + week + 1 } }, base + week);
assert.equal(expired(marker, base + week * 3), false, 'explicit restore protects entry');
editLocalPodcast(ep, 'completion', true, base);
assert.equal(localPodcastSnapshot(ep, base + week - 1).deletion, undefined);
assert.equal(localPodcastSnapshot(ep, base + week).deletion.deleted, true);
assert.equal(localPodcastSnapshot(ep, base + week).completion.completed, true, 'history is retained');
editLocalPodcast(ep, 'deletion', false, base + week + 1);
assert.equal(localPodcastSnapshot(ep, base + week * 2).deletion.deleted, false);
const state = await import('../podcast-episode-state.js');
state.setPodcastAccount(9);
state.completionEdit(ep, true, base);
state.sweepPodcastRetention(base + week - 1);
assert.equal(state.episodeSnapshot(ep, base + week - 1).deletion, null);
state.sweepPodcastRetention(base + week);
assert.equal(state.episodeSnapshot(ep, base + week).deletion.deleted, true);
assert.equal(state.episodeSnapshot(ep).completion.completed, true);
assert.equal(state.podcastSyncStatus().pending, 1, 'device expiry must not enqueue a manual deletion');
state.setPodcastAccount(10);
assert.equal(state.episodeSnapshot(ep).deletion, null);
state.setPodcastAccount(9);
assert.equal(state.episodeSnapshot(ep, base + week).deletion.deleted, true, 'derived expiry survives account reload');
const derived = state.episodeSnapshot(ep, base + week);
assert.equal(state.episodeSnapshot(derived, base).deletion, null, 'derived hiding never becomes a sync register');


state.applyPodcastPull({ episodes: [{ ...ep, position: { seconds: 1, observed_ms: base + 1 } }] });
assert.equal(state.episodeSnapshot(ep, base + week).deletion, null, 'newer remote playback reopens device expiry');

console.log('Podcast retention boundaries, replay/restore guards, offline durability and account isolation passed.');
const { localPodcastSourceURL } = await import('../streamSource.js');
globalThis.location = { origin: 'https://server.example', search: '?local=podcasts' };
const local = { localPodcast: true, localPodcastURL: 'https://publisher.example/episode.mp3' };
assert.equal(localPodcastSourceURL(local), local.localPodcastURL);
assert.equal(localPodcastSourceURL({ ...local, localPodcastURL: 'javascript:alert(1)' }), null);
assert.equal(localPodcastSourceURL({ ...local, localPodcastURL: 'https://user:password@publisher.example/one.mp3' }), null);
location.search = '';
assert.equal(localPodcastSourceURL(local), null, 'device enclosure sources never bypass authenticated source handling');
state.setPodcastAccount(11);
state.episodeSnapshot({ ...ep, completion: { completed: false, source: 'manual', observed_ms: base } });
const firstSeen = read(11)[0].seen_ms;
state.episodeSnapshot({ ...ep, completion: { completed: true, source: 'manual', observed_ms: base } });
assert.equal(read(11)[0].completed, true, 'same-clock authoritative true replaces false');
assert.equal(read(11)[0].seen_ms, firstSeen, 'same-clock correction preserves first observation');
state.episodeSnapshot({ ...ep, completion: { completed: false, source: 'manual', observed_ms: base } });
assert.equal(read(11)[0].completed, false, 'same-clock authoritative false replaces true');
assert.equal(state.episodeSnapshot(ep, firstSeen + week).deletion, null);
// Sparse/interrupted pulls after reload cannot erase independent replay or
// restore clocks merely by advancing the completion register.
for (const [account, guard] of [[21, 'position'], [23, 'deletion']]) {
  state.setPodcastAccount(account);
  state.episodeSnapshot({ ...ep, server_copy: { media_id: 991 },
    completion: { completed: true, observed_ms: 100 },
    [guard]: guard === 'position' ? { seconds: 1, observed_ms: 300 } : { deleted: false, observed_ms: 300 } });
  state.setPodcastAccount(account + 1); state.setPodcastAccount(account);
  state.applyPodcastPull({ episodes: [{ ...ep, completion: { completed: true, observed_ms: 200 } }], has_more: true });
  let retained = read(account)[0];
  assert.equal(retained[`${guard}_ms`], 300, 'sparse newer completion retains durable independent guard');
  assert.equal(retained.media_id, 991, 'sparse completion retains physical copy identity');
  assert.equal(state.episodeSnapshot(ep, retained.seen_ms + week).deletion, null);
  state.setPodcastAccount(account + 1); state.setPodcastAccount(account);
  assert.equal(state.episodeSnapshot(ep, retained.seen_ms + week).deletion, null, 'interrupted pull survives another reload');
  state.applyPodcastPull({ episodes: [{ ...ep, completion: { completed: true, observed_ms: 400 } }] });
  retained = read(account)[0];
  assert.equal(state.episodeSnapshot(ep, retained.seen_ms + week - 1).deletion, null);
  assert.equal(state.episodeSnapshot(ep, retained.seen_ms + week).deletion.deleted, true, 'completion newer than every guard expires after fresh grace');
}
const { remapPodcastRetention } = await import('../podcast-retention.js');
for (const [account, sourceClock, targetClock] of [[25, 200, 100], [26, 100, 200], [27, 200, 200]]) {
  const source = { ...ep, episode_key: 'alias' }, target = { ...ep, episode_key: 'canonical' };
  observe(account, { ...source, completion: { completed: true, observed_ms: sourceClock }, position: { observed_ms: 500 }, ...(sourceClock <= targetClock ? { server_copy: { media_id: 992 } } : {}) }, base);
  observe(account, { ...target, completion: { completed: true, observed_ms: targetClock }, deletion: { deleted: false, observed_ms: 600 }, ...(sourceClock > targetClock ? { server_copy: { media_id: 992 } } : {}) }, base + 1);
  remapPodcastRetention(account, [{ kind: 'episode', feed_key: ep.feed_key, from: source.episode_key, to: target.episode_key }]);
  const merged = read(account);
  assert.equal(merged.length, 1);
  assert.equal(merged[0].completion_ms, Math.max(sourceClock, targetClock));
  assert.equal(merged[0].seen_ms, sourceClock >= targetClock ? base : base + 1);
  assert.equal(merged[0].position_ms, 500);
  assert.equal(merged[0].deletion_ms, 600);
  assert.equal(merged[0].media_id, 992);
  assert.equal(expired(merged[0], base + week * 2), false);
  const next = observe(account, { ...target, completion: { completed: true, observed_ms: 700 } }, base + week * 2);
  assert.equal(next.media_id, 992);
  assert.equal(expired(next, next.seen_ms + week - 1), false);
  assert.equal(expired(next, next.seen_ms + week), true);
}
console.log('Sparse completion/reload guards and alias collisions preserve independent clocks and physical identity.');
