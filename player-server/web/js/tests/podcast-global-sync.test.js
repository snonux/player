import assert from 'node:assert/strict';
import { webcrypto } from 'node:crypto';
import { API } from '../api.js';
import { setPodcastAccount, episodeSnapshot, completionEdit, podcastSyncStatus, applyPodcastPull } from '../podcast-episode-state.js';
import { subscriptionEdit, pendingPodcastSubscriptions } from '../podcast-subscriptions.js';
import { syncPodcastsForGlobal } from '../podcast-global-sync.js';
Object.defineProperty(globalThis, 'crypto', { value: webcrypto, configurable: true });
const ep = { feed_key: 'feed:example.com/rss', episode_key: 'guid:a' };
const feed = { ...ep, feed_url: 'https://example.com/rss' };
const page = (cursor, more = false) => ({ epoch: 'one', next_since: cursor, has_more: more, episodes: [], subscriptions: [], rejected: [], acked: [] });
setPodcastAccount(1);
API.syncConfig = async () => ({ user_id: 1, podcast_sync: 1 });
completionEdit(ep, true, 100); subscriptionEdit(feed, false, 100);
const ids = [...podcastSyncStatus().pendingIds, pendingPodcastSubscriptions()[0].edit_id];
let pulls = 0;
API.syncPodcastSubscriptions = async edits => ({ ...page(0), acked: edits.map(edit => edit.edit_id) });
API.syncPodcast = async body => {
  if (body.scope) return { ...page(0), acked: body.episodes.flatMap(row => ['position', 'completion'].flatMap(name => row[name] ? [row[name].edit_id] : [])) };
  pulls++;
  if (pulls === 1) { assert.equal(body.since, 0); return { ...page(7, true), episodes: [{ ...ep, position: { seconds: 42, observed_ms: 1000 } }] }; }
  assert.equal(body.since, 7); assert.equal(body.epoch, 'one'); return page(8);
};
const committed = [];
await syncPodcastsForGlobal(() => true, async p => committed.push(p.next_since), { epoch: '', since: 0 });
assert.deepEqual(committed, [7, 8]); assert.equal(pulls, 2);
assert.equal(episodeSnapshot(ep).position.seconds, 42);
assert.equal(podcastSyncStatus().pendingIds.some(id => ids.includes(id)), false);
assert.equal(pendingPodcastSubscriptions().length, 0);

// A failed push still permits committed pull pages, but cannot count as full.
completionEdit(ep, false, 2000);
let pushedID;
API.syncPodcast = async body => {
  if (body.scope) { pushedID = body.episodes[0].completion.edit_id; throw new TypeError('offline'); }
  return page(9);
};
await assert.rejects(syncPodcastsForGlobal(() => true, async p => committed.push(p.next_since), { epoch: 'one', since: 8 }));
assert.equal(committed.at(-1), 9); assert.ok(podcastSyncStatus().pendingIds.includes(pushedID));

// Alias collision keeps a newer clean winner, with a fresh replay edit.
const canonical = { ...ep, episode_key: 'guid:canonical' };
episodeSnapshot({ ...canonical, completion: { completed: true, source: 'manual', observed_ms: 3000 } });
applyPodcastPull({ aliases: [{ kind: 'episode', feed_key: ep.feed_key, from: ep.episode_key, to: canonical.episode_key }] });
assert.equal(episodeSnapshot(canonical).completion.completed, true);
assert.equal(podcastSyncStatus().pending, 1);
assert.notEqual(podcastSyncStatus().pendingIds[0], pushedID);

// A restored server interrupted halfway through its pull retains tab-local
// unconfirmed markers; resumption replays clean missing values at their clock.
setPodcastAccount(4); API.syncConfig = async () => ({ user_id: 4, podcast_sync: 1 });
episodeSnapshot({ ...ep, position: { seconds: 50, observed_ms: 500 } });
let cursor = { epoch: 'one', since: 0 }, interrupted = true, replay;
API.syncPodcast = async body => {
  if (body.scope) { replay = body.episodes[0].position; return { ...page(0), acked: [replay.edit_id], episodes: [{ ...ep, position: replay }] }; }
  if (body.since === 0) return { ...page(1, true), epoch: 'restored', episodes: [{ ...ep, position: { seconds: 10, observed_ms: 100 } }] };
  if (interrupted) throw new TypeError('pull interrupted');
  return { ...page(2), epoch: 'restored' };
};
const commit = async p => { cursor = { epoch: p.epoch, since: p.next_since }; };
await assert.rejects(syncPodcastsForGlobal(() => true, commit, cursor));
assert.deepEqual(cursor, { epoch: 'restored', since: 1 });
interrupted = false;
await syncPodcastsForGlobal(() => true, commit, cursor);
assert.equal(replay.seconds, 50); assert.equal(replay.observed_ms, 500);
assert.equal(podcastSyncStatus().pending, 0);

// A switch during a pull prevents merge and cursor commit in the new account.
setPodcastAccount(2); API.syncConfig = async () => ({ user_id: 2, podcast_sync: 1 });
let release;
API.syncPodcast = () => new Promise(resolve => { release = resolve; });
let current = true, commits = 0;
const running = syncPodcastsForGlobal(() => current, async () => commits++, { epoch: '', since: 0 });
while (!release) await Promise.resolve();
setPodcastAccount(3); current = false;
release({ ...page(10), episodes: [{ ...ep, position: { seconds: 999, observed_ms: 9999 } }] });
await running; assert.equal(commits, 0); assert.equal(episodeSnapshot(ep).position, null);
setPodcastAccount(null);
console.log('Actual podcast retries, pagination, alias replay, partial failure and account checks passed.');
