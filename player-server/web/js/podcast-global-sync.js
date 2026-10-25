import { API } from './api.js';
import { podcastSyncStatus, retryPodcastEpisodeEdits, applyPodcastPull, finishPodcastPull } from './podcast-episode-state.js';
import { retryPodcastSubscriptions, pendingPodcastSubscriptions } from './podcast-subscriptions.js';

let subscriptionGeneration, subscriptionRegisters = new Map();

export async function syncPodcastsForGlobal(current, committed, cursor) {
  const lease = podcastSyncStatus();
  if (subscriptionGeneration !== lease.generation) {
    subscriptionGeneration = lease.generation; subscriptionRegisters = new Map();
  }
  const initialIds = [...lease.pendingIds, ...pendingPodcastSubscriptions().map(edit => edit.edit_id)];
  const cfg = await API.syncConfig();
  if (!current() || cfg.user_id !== lease.account) throw new Error('Account changed');
  if (cfg.podcast_sync !== 1) return { excluded: initialIds };
  let failure;
  for (const retry of [retryPodcastSubscriptions, retryPodcastEpisodeEdits]) {
    if (!current()) return;
    try { await retry(); } catch (error) { failure ||= error; }
  }
  while (current()) {
    const page = await API.syncPodcast({ ...cursor, limit: 500 });
    if (!current()) return;
    if (typeof page.has_more !== 'boolean' || !Number.isSafeInteger(page.next_since) || typeof page.epoch !== 'string') throw new Error('Incomplete podcast response');
    if (!applyPodcastPull(page, lease.generation, { epochChanged: page.epoch !== cursor.epoch })) return;
    if (page.rejected?.length) failure ||= Object.assign(new Error('Podcast changes rejected'), { kind: 'rejected' });
    await committed(page);
    let subscriptionsChanged = false;
    for (const row of page.subscriptions || []) {
      const encoded = JSON.stringify(row);
      if (subscriptionRegisters.get(row.feed_key) !== encoded) subscriptionsChanged = true;
      subscriptionRegisters.set(row.feed_key, encoded);
    }
    if (subscriptionsChanged) globalThis.document?.dispatchEvent(new Event('podcast:subscriptions-changed'));
    if (!page.has_more) break;
    if (page.epoch === cursor.epoch && page.next_since <= cursor.since) throw new Error('Podcast pull did not advance');
    cursor = { epoch: page.epoch, since: page.next_since };
  }
  if (current()) {
    finishPodcastPull(lease.generation);
    await retryPodcastEpisodeEdits();
  }
  if (failure) throw failure;
}
