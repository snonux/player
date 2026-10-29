import assert from 'node:assert/strict';
import { webcrypto } from 'node:crypto';
import { API } from '../api.js';
import { podcastDeletionStorageKey, readPodcastDeletions } from '../podcast-deletion-store.js';
Object.defineProperty(globalThis, 'crypto', { value: webcrypto, configurable: true });
const storage = new Map();
globalThis.localStorage = {
  getItem: key => storage.get(key) ?? null,
  setItem: (key, value) => storage.set(key, value),
  get length() { return storage.size; },
  key: i => [...storage.keys()][i] ?? null,
  removeItem: key => storage.delete(key),
};
const fresh = async label => import(`../podcast-episode-state.js?deletion-test=${label}`);
let state = await fresh('first');
const ep = { feed_key: 'feed:example.com/rss', episode_key: 'guid:one' };
const register = (deleted, observed_ms) => ({ deleted, observed_ms });
const server = row => ({ episodes: row ? [row] : [], acked: [], rejected: [] });
API.syncConfig = async () => ({ user_id: 1, podcast_episode_deletion: true });
state.setPodcastAccount(1);
state.episodeSnapshot({ ...ep, position: { seconds: 234, observed_ms: 20 }, completion: { completed: true, source: 'manual', observed_ms: 30 } });
API.syncPodcast = async () => { throw new Error('offline'); };
await assert.rejects(state.setEpisodeDeleted(ep), /offline/);
const pendingID = state.podcastSyncStatus().pendingIds[0];
const saved = readPodcastDeletions(1)[0];
assert.equal(saved.pending.edit_id, pendingID);
assert.equal(saved.deletion.deleted, true);
assert.equal(saved.position, undefined, 'durable deletion store does not copy listening history');
assert.equal(state.episodeSnapshot(ep).position.seconds, 234);
assert.equal(state.episodeSnapshot(ep).completion.completed, true);

// A new module has no tab memory. Its only source is the actual saved store.
state = await fresh('reload'); state.setPodcastAccount(1);
assert.equal(state.episodeSnapshot(ep).deletion.deleted, true);
assert.deepEqual(state.podcastSyncStatus().pendingIds, [pendingID]);
let sent;
API.syncPodcast = async body => {
  sent = body.episodes[0];
  return { ...server({ ...ep, deletion: { ...sent.deletion } }), acked: [sent.deletion.edit_id] };
};
await state.retryPodcastEpisodeEdits();
assert.equal(sent.deletion.edit_id, pendingID, 'reload retries the exact edit identity and clock');
assert.equal(sent.deletion.observed_ms, saved.pending.observed_ms);
assert.equal(state.podcastSyncStatus().pending, 0);
assert.equal(readPodcastDeletions(1)[0].pending, undefined);
const clock = state.episodeSnapshot(ep).deletion.observed_ms;
state.applyPodcastPull(server({ ...ep, deletion: register(false, clock) }));
assert.equal(state.episodeSnapshot(ep).deletion.deleted, true, 'deletion wins a positive equal-clock pull');
state.applyPodcastPull(server({ ...ep, deletion: register(false, clock - 1) }));
assert.equal(state.episodeSnapshot(ep).deletion.deleted, true, 'stale refresh cannot resurrect episode');

// Aliases collapse registers without losing deletion on a same-clock tie.
const canonical = { ...ep, episode_key: 'guid:canonical' };
state.episodeSnapshot({ ...canonical, deletion: register(false, clock) });
state.applyPodcastPull({ aliases: [{ kind: 'episode', feed_key: ep.feed_key, from: ep.episode_key, to: canonical.episode_key }] });
assert.equal(state.episodeSnapshot(canonical).deletion.deleted, true);
state = await fresh('canonical-reload'); state.setPodcastAccount(1);
assert.equal(state.episodeSnapshot(canonical).deletion.deleted, true, 'canonical tombstone survives reload');

// Recovery replays a known true tombstone if a restored server has a false tie.
state.applyPodcastPull({ ...server({ ...canonical, deletion: register(false, clock) }), epoch: 'restored' }, undefined, { epochChanged: true });
state.finishPodcastPull();
assert.equal(state.podcastSyncStatus().pending, 1);
state.setPodcastAccount(2);
assert.equal(state.episodeSnapshot(canonical).deletion, null, 'accounts do not inherit tombstones');
state.setPodcastAccount(1);
assert.equal(state.episodeSnapshot(canonical).deletion.deleted, true);
assert.equal(state.podcastSyncStatus().pending, 1, 'epoch recovery replay is durable');

// An old server must not see unknown fields or block independent completion.
API.syncConfig = async () => ({ user_id: 1, podcast_sync: 1 });
state.completionEdit(canonical, true, clock + 1);
API.syncPodcast = async body => { sent = body.episodes[0]; return { ...server(), acked: [sent.completion.edit_id] }; };
await assert.rejects(state.syncEpisode(canonical, true), /Update this server/);
assert.equal(sent.deletion, undefined);
assert.equal(sent.completion.completed, true);
assert.equal(state.podcastSyncStatus().pending, 1, 'unsupported deletion remains pending');

// A storage failure must not remove the episode optimistically or send it.
state.setPodcastAccount(3);
const setItem = localStorage.setItem;
localStorage.setItem = () => { throw new Error('storage full'); };
assert.throws(() => state.deletionEdit(ep), /storage full/);
assert.equal(state.episodeSnapshot(ep).deletion, null);
assert.equal(state.podcastSyncStatus().pending, 0);
localStorage.setItem = setItem;

// Quota rejection retains a real durable retry rather than a hidden clean row.
state.setPodcastAccount(4);
API.syncConfig = async () => ({ user_id: 4, podcast_episode_deletion: true });
let rejectedID;
API.syncPodcast = async body => {
  rejectedID = body.episodes[0].deletion.edit_id;
  return { ...server(), rejected: [{ edit_id: rejectedID, reason: 'quota' }] };
};
await assert.rejects(state.setEpisodeDeleted(ep), /quota/);
assert.deepEqual(state.podcastSyncStatus().pendingIds, [rejectedID]);
state = await fresh('quota-reload'); state.setPodcastAccount(4);
assert.deepEqual(state.podcastSyncStatus().pendingIds, [rejectedID]);
API.syncPodcast = async body => {
  assert.equal(body.episodes[0].deletion.edit_id, rejectedID);
  return { ...server({ ...ep, deletion: body.episodes[0].deletion }), acked: [rejectedID] };
};
await state.retryPodcastEpisodeEdits();
assert.equal(state.podcastSyncStatus().pending, 0);

// Existing tabs can interleave writes before either receives a storage event.
const tabA = await fresh('tab-a'), tabB = await fresh('tab-b');
tabA.setPodcastAccount(11); tabB.setPodcastAccount(11);
const other = { ...ep, episode_key: 'guid:two' };
let interleaved = false;
localStorage.setItem = (key, value) => {
  if (!interleaved) { interleaved = true; tabB.deletionEdit(other, true, 100); }
  setItem(key, value);
};
tabA.deletionEdit(ep, true, 100);
localStorage.setItem = setItem;
const reloadedTabs = await fresh('tabs-reload'); reloadedTabs.setPodcastAccount(11);
assert.equal(reloadedTabs.episodeSnapshot(ep).deletion.deleted, true);
assert.equal(reloadedTabs.episodeSnapshot(other).deletion.deleted, true);
assert.equal(reloadedTabs.podcastSyncStatus().pending, 2, 'both concurrent pending edit identities survive reload');

// Concurrent true/false at one clock use separate immutable observations.
const raceA = await fresh('race-a'), raceB = await fresh('race-b');
raceA.setPodcastAccount(12); raceB.setPodcastAccount(12);
interleaved = false;
localStorage.setItem = (key, value) => {
  if (!interleaved) { interleaved = true; raceB.deletionEdit(ep, false, 200); }
  setItem(key, value);
};
raceA.deletionEdit(ep, true, 200);
localStorage.setItem = setItem;
const raced = await fresh('race-reload'); raced.setPodcastAccount(12);
assert.equal(raced.episodeSnapshot(ep).deletion.deleted, true, 'true wins a same-clock cross-tab write race');
assert.equal(raced.podcastSyncStatus().pending, 1);
const journalSize = storage.size;
raced.episodeSnapshot(ep); // Reading and repeating the same pending edit add no journal entries.
raced.deletionEdit(ep, true, 200);
assert.equal(storage.size, journalSize);

// A stale tab can append a newer old-key edit during canonical persistence.
const aliasA = await fresh('alias-race-a'), aliasB = await fresh('alias-race-b');
aliasA.setPodcastAccount(13); aliasB.setPodcastAccount(13);
aliasA.episodeSnapshot({ ...ep, deletion: register(false, 200) });
aliasB.episodeSnapshot({ ...ep, deletion: register(false, 200) });
interleaved = false;
localStorage.setItem = (key, value) => {
  if (!interleaved && JSON.parse(value).episode_key === canonical.episode_key) {
    interleaved = true; aliasB.deletionEdit(ep, true, 300);
  }
  setItem(key, value);
};
aliasA.applyPodcastPull({ aliases: [{ kind: 'episode', feed_key: ep.feed_key, from: ep.episode_key, to: canonical.episode_key }] });
localStorage.setItem = setItem;
const aliasReload = await fresh('alias-race-reload'); aliasReload.setPodcastAccount(13);
assert.equal(aliasReload.episodeSnapshot(canonical).deletion.deleted, true, 'concurrent newer alias deletion survives canonical write');
assert.equal(aliasReload.episodeSnapshot(canonical).deletion.observed_ms, 300);
assert.equal(aliasReload.podcastSyncStatus().pending, 1, 'canonical replay retains the stale tab exact edit');

// The server may clamp a future device clock while acknowledging its edit.
state = await fresh('clamp'); state.setPodcastAccount(21);
API.syncConfig = async () => ({ user_id: 21, podcast_episode_deletion: true });
state.deletionEdit(ep, true, Date.now() + 3600000);
API.syncPodcast = async body => ({ ...server({ ...ep, deletion: register(true, Date.now()) }), acked: [body.episodes[0].deletion.edit_id] });
await state.syncEpisode(ep, true);
state = await fresh('clamp-reload'); state.setPodcastAccount(21);
assert.equal(state.podcastSyncStatus().pending, 0, 'acknowledged clock-clamped deletion must not revive a pending edit');

// An old response retires only the sent edit, never a newer local edit.
state = await fresh('late-ack'); state.setPodcastAccount(22);
API.syncConfig = async () => ({ user_id: 22, podcast_episode_deletion: true });
let release;
API.syncPodcast = body => new Promise(resolve => {
  release = () => resolve({ ...server({ ...ep, deletion: register(true, 100) }), acked: [body.episodes[0].deletion.edit_id] });
});
state.deletionEdit(ep, true, 100);
const inflight = state.syncEpisode(ep, true);
while (!release) await Promise.resolve();
const newer = state.deletionEdit(ep, false, 200).deletion.edit_id;
release(); await inflight;
state = await fresh('late-ack-reload'); state.setPodcastAccount(22);
assert.deepEqual(state.podcastSyncStatus().pendingIds, [newer]);
assert.equal(state.episodeSnapshot(ep).deletion.deleted, false);
assert.equal(state.episodeSnapshot(ep).deletion.observed_ms, 200);

// A live second tab must honor an exact receipt even when server clamps lower.
const storageHandlers = [], oldAddEventListener = globalThis.addEventListener;
globalThis.addEventListener = (type, handler) => { if (type === 'storage') storageHandlers.push(handler); };
const ackA = await fresh('storage-ack-a'); ackA.setPodcastAccount(23);
const oldID = ackA.deletionEdit(ep, true, Date.now() + 3600000).deletion.edit_id;
const ackB = await fresh('storage-ack-b'); ackB.setPodcastAccount(23);
const onStorage = storageHandlers.at(-1);
API.syncConfig = async () => ({ user_id: 23, podcast_episode_deletion: true });
API.syncPodcast = async () => ({ ...server({ ...ep, deletion: register(true, Date.now()) }), acked: [oldID] });
await ackA.syncEpisode(ep, true);
onStorage({ key: `${podcastDeletionStorageKey(23)}:ack:${oldID}` });
assert.equal(ackB.podcastSyncStatus().pending, 0, 'live tab retires the exact acknowledged future-clock pending edit');
assert.ok(ackB.episodeSnapshot(ep).deletion.observed_ms < Date.now() + 3600000);
const stillPending = ackB.deletionEdit(ep, false, Date.now() + 60000).deletion.edit_id;
onStorage({ key: `${podcastDeletionStorageKey(23)}:ack:${oldID}` });
assert.deepEqual(ackB.podcastSyncStatus().pendingIds, [stillPending], 'old receipt storage event preserves a new pending identity');
globalThis.addEventListener = oldAddEventListener;

// A distinct origin has a distinct localStorage namespace.
globalThis.localStorage = { length: 0, key: () => null, getItem: () => null, setItem() {} };
state = await fresh('other-origin'); state.setPodcastAccount(1);
assert.equal(state.episodeSnapshot(ep).deletion, null);
state.setPodcastAccount(null);
console.log('Podcast deletion durability, account/origin isolation, clock ties, alias/epoch recovery and compatibility passed.');
