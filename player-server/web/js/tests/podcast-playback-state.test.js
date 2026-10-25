import assert from 'node:assert/strict';
import { webcrypto } from 'node:crypto';
import { API } from '../api.js';
import { setPodcastAccount, episodeSnapshot, syncEpisode, reconcileEpisode, saveEpisodePosition,
  completionEdit, setEpisodeCompleted, podcastSyncStatus, pendingCompletionEdit, podcastStartPosition } from '../podcast-episode-state.js';
import { preparePodcastPlayback, podcastNaturalEnd } from '../podcast-playback.js';
globalThis.crypto ??= webcrypto;
const ep = { feed_key: 'feed:example.com/rss', episode_key: 'guid:1', position_seconds: 42 };
const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
const tick = () => new Promise(resolve => setImmediate(resolve));
const response = (body, state) => ({ acked: (body.episodes || []).flatMap(item => [item.position?.edit_id, item.completion?.edit_id].filter(Boolean)), rejected: [], episodes: state ? [state] : [] });

setPodcastAccount(1);
API.mediaDetail = async () => ({ podcast_episode: { ...ep, position: { seconds: 123, observed_ms: 100 } } });
API.syncPodcast = async body => response(body, { ...ep, position: { seconds: 234, observed_ms: 200 }, completion: { completed: true, source: 'manual', observed_ms: 300 } });
const prepared = await preparePodcastPlayback({ id: 9 }, 0, 20);
assert.equal(prepared.position, 234, 'identity discovered through generic media detail resumes shared Android position');
assert.equal(prepared.episode.completion.completed, true, 'replaying does not uncomplete');
assert.equal(podcastStartPosition(234, 100), 0, 'loaded duration owns end-position rule');
assert.equal(podcastStartPosition(234, 1000), 234, 'incorrect publisher duration does not force restart');
assert.equal(podcastStartPosition(234, 1000, true), 0);

setPodcastAccount(2);
const slow = deferred();
API.syncPodcast = () => slow.promise;
const fallback = await reconcileEpisode(ep, 5);
assert.equal(fallback.position.seconds, 42, 'bounded startup uses local fallback');
const owner = podcastSyncStatus().generation;
saveEpisodePosition(ep, 60, { now: 2000, lease: owner });
slow.resolve({ acked: [], rejected: [], episodes: [{ ...ep, position: { seconds: 100, observed_ms: 1000 } }] });
await tick();
assert.equal(episodeSnapshot(ep).position.seconds, 60, 'late pull cannot replace newer pending playback');
assert.equal(fallback.position.seconds, 42, 'late pull cannot mutate chosen source snapshot');

setPodcastAccount(3);
episodeSnapshot({ ...ep, position: { seconds: 10, observed_ms: 9000 }, completion: { completed: false, source: 'manual', observed_ms: 12000 } });
const blocked = deferred(), requests = [];
API.syncPodcast = body => { requests.push(body); return blocked.promise; };
saveEpisodePosition(ep, 600, { finished: true, now: 100 });
const end = requests[0].episodes[0];
assert.equal(end.position.observed_ms, 12001);
assert.equal(end.position.observed_ms, end.completion.observed_ms, 'end writes both registers at one monotonic clock');
assert.equal(end.completion.source, 'auto');
assert.equal(episodeSnapshot(ep).completion.completed, true, 'local completion committed before blocked transport');
assert.equal(podcastSyncStatus().pending, 2);

// HTML ended can expose paused=true. The actual guard uses prior advancement,
// and terminal position is enough even with no preceding timeupdate sample.
assert.equal(podcastNaturalEnd({ played: true, advancing: true, paused: true, position: 600, target: 599.95 }), true);
for (const change of [{ target: 600 }, { seeking: true }, { advancing: false }, { played: false }, { error: {} }]) {
  assert.equal(podcastNaturalEnd({ played: true, advancing: true, position: 600, target: 10, ...change }), false);
}

setPodcastAccount(4);
episodeSnapshot({ ...ep, position: { seconds: 600, observed_ms: 1000 } });
API.syncPodcast = async body => response(body, { ...ep, position: { seconds: 600, observed_ms: 1000 }, completion: { completed: false, source: 'manual', observed_ms: Date.now() + 100 } });
const superseded = await setEpisodeCompleted(ep, true);
assert.equal(superseded.completion.completed, false, 'acknowledged superseded manual edit adopts server truth');
assert.equal(superseded.position.seconds, 600, 'manual edit preserves position');
assert.equal(pendingCompletionEdit(ep.feed_key, ep.episode_key), undefined);

setPodcastAccount(5);
let failedBody;
API.syncPodcast = async body => { failedBody = body; throw new Error('network'); };
await assert.rejects(setEpisodeCompleted(ep, true));
const failedId = failedBody.episodes[0].completion.edit_id;
API.syncPodcast = async body => { assert.equal(body.episodes[0].completion.edit_id, failedId, 'retry keeps exact edit id'); return response(body, { ...ep, completion: { ...body.episodes[0].completion } }); };
await setEpisodeCompleted(ep, true);

const late = deferred();
API.syncPodcast = () => late.promise;
const oldLease = podcastSyncStatus().generation;
const waiting = reconcileEpisode(ep, 30);
setPodcastAccount(6);
late.resolve({ acked: [], rejected: [], episodes: [{ ...ep, position: { seconds: 999, observed_ms: 99999 } }] });
assert.equal(await waiting, null, 'old bounded attempt does not recreate old-account storage');
assert.equal(saveEpisodePosition(ep, 333, { lease: oldLease }), null, 'old player callbacks cannot write new account');
assert.equal(podcastSyncStatus().pending, 0);
assert.equal(episodeSnapshot({ ...ep, position_seconds: 0 }).position, null);
blocked.resolve({ acked: [], rejected: [], episodes: [] });
await tick();
assert.equal(podcastSyncStatus().pending, 0, 'old end response stays isolated');

setPodcastAccount(7);
const canonical = { ...ep, episode_key: 'guid:canonical', position_seconds: 0 };
API.syncPodcast = () => new Promise(() => {});
saveEpisodePosition(canonical, 55, { now: 10000 });
const targetEdit = completionEdit(canonical, false, 11000).completion.edit_id;
completionEdit(ep, true, 1000);
API.syncPodcast = async body => ({ ...response(body, { ...canonical,
  position: { seconds: 11, observed_ms: 1000 }, completion: { completed: true, source: 'manual', observed_ms: 1000 } }),
  resolved: [{ kind: 'episode', feed_key: ep.feed_key, from: ep.episode_key, to: canonical.episode_key }] });
await syncEpisode(ep);
assert.equal(episodeSnapshot(ep).position.seconds, 55, 'alias collision retains newer target position');
assert.equal(episodeSnapshot(canonical).completion.completed, false, 'alias collision retains target completion');
assert.equal(pendingCompletionEdit(canonical.feed_key, canonical.episode_key).completion.edit_id, targetEdit, 'canonical pending identity survives collision');
assert.equal(podcastSyncStatus().pending, 2, 'aliases point at one merged pair of pending registers');
setPodcastAccount(null);
console.log('podcast playback state tests passed');
