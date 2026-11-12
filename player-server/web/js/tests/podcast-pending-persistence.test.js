// Progress made while the server is down survives a reload and is sent later:
// position, completion and deletion of a podcast episode.
import assert from 'node:assert/strict';
import { webcrypto } from 'node:crypto';
import { API } from '../api.js';
import { podcastPendingStorageKey, writePodcastPendingEdits } from '../podcast-pending-store.js';
Object.defineProperty(globalThis, 'crypto', { value: webcrypto, configurable: true });
const storage = new Map();
globalThis.localStorage = {
  getItem: key => storage.get(key) ?? null,
  setItem: (key, value) => storage.set(key, value),
  get length() { return storage.size; },
  key: i => [...storage.keys()][i] ?? null,
  removeItem: key => storage.delete(key),
};
const fresh = async label => import(`../podcast-episode-state.js?pending-test=${label}`);
const ep = { feed_key: 'feed:example.com/rss', episode_key: 'guid:one' };
const other = { feed_key: 'feed:example.com/rss', episode_key: 'guid:two' };
API.syncConfig = async () => ({ user_id: 1, podcast_episode_deletion: true });

// Server down: every edit fails and stays pending.
let state = await fresh('offline');
state.setPodcastAccount(1);
API.syncPodcast = async () => { throw new Error('server unreachable'); };
state.saveEpisodePosition(ep, 1234, { now: 5000 });
await assert.rejects(state.setEpisodeCompleted(other, true), /server unreachable/);
await assert.rejects(state.setEpisodeDeleted(other, true), /server unreachable/);
await new Promise(resolve => setTimeout(resolve, 0));
assert.equal(state.podcastSyncStatus().pending, 3);
assert.ok(storage.get(podcastPendingStorageKey(1)), 'unsent progress is saved in the browser');

// The tab is reloaded while the server is still down.
state = await fresh('reloaded');
state.setPodcastAccount(1);
assert.equal(state.podcastSyncStatus().pending, 3, 'position, completion and deletion survive a reload');
assert.equal(state.episodeSnapshot(ep).position.seconds, 1234, 'resume uses the unsent position');
assert.equal(state.episodeSnapshot(other).completion.completed, true);
assert.equal(state.episodeSnapshot(other).deletion.deleted, true);

// A newer server register (another device) is not overwritten locally by an
// older saved edit, but the edit is still sent and the server decides.
state.episodeSnapshot({ ...ep, position: { seconds: 99, observed_ms: 4000 } });
assert.equal(state.episodeSnapshot(ep).position.seconds, 1234, 'pending edit wins until answered');

// Server is back: the retry sends everything and the saved edits are cleared.
const sent = [];
API.syncPodcast = async body => {
  sent.push(...body.episodes);
  const acked = body.episodes.flatMap(row => ['position', 'completion', 'deletion'].filter(name => row[name]).map(name => row[name].edit_id));
  return { acked, rejected: [], episodes: body.episodes.map(row => ({ ...row })) };
};
await state.retryPodcastEpisodeEdits();
assert.equal(sent.find(row => row.episode_key === ep.episode_key)?.position?.seconds, 1234);
assert.equal(sent.find(row => row.episode_key === other.episode_key && row.completion)?.completion?.completed, true);
assert.equal(sent.find(row => row.episode_key === other.episode_key && row.deletion)?.deletion?.deleted, true);
assert.equal(sent.find(row => row.position)?.position?.observed_ms, 5000, 'original clock is kept for last-writer-wins');
assert.equal(state.podcastSyncStatus().pending, 0);
assert.equal(storage.get(podcastPendingStorageKey(1)), undefined, 'answered edits leave the browser store');

// Another tab's unsent edit is kept when this tab writes its own.
const rows = new Map([['x', { feed_key: 'f', episode_key: 'a', pending: { position: { edit_id: 'a'.repeat(16), seconds: 1, observed_ms: 1 } } }]]);
writePodcastPendingEdits(2, rows);
const rows2 = new Map([['y', { feed_key: 'f', episode_key: 'b', pending: { position: { edit_id: 'b'.repeat(16), seconds: 2, observed_ms: 2 } } }]]);
writePodcastPendingEdits(2, rows2);
assert.equal(Object.keys(JSON.parse(storage.get(podcastPendingStorageKey(2))).episodes).length, 2);
writePodcastPendingEdits(2, new Map(), ['a'.repeat(16)]);
assert.equal(Object.keys(JSON.parse(storage.get(podcastPendingStorageKey(2))).episodes).length, 1);

console.log('podcast pending persistence tests passed');
