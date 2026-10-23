// podcast-episode-state.js — explicit Mark completed / Mark uncompleted
// through POST /api/podcasts/sync (contract §6.2). Idempotent retries reuse
// the same edit_id until the server acknowledges it.
import { API } from './api.js';
import { randomEditId } from './podcast-subscriptions.js';

const pending = new Map(); // `${feed_key}\0${episode_key}` -> episode item

function episodePendingKey(feedKey, episodeKey) {
  return `${feedKey}\0${episodeKey}`;
}

// completionEdit builds (or reuses) a manual completion edit for ep.
export function completionEdit(ep, completed, now = Date.now()) {
  const key = episodePendingKey(ep.feed_key, ep.episode_key);
  const old = pending.get(key);
  if (old?.completion && old.completion.completed === completed) return old;
  const edit = {
    feed_key: ep.feed_key,
    episode_key: ep.episode_key,
    completion: {
      edit_id: randomEditId(),
      completed: Boolean(completed),
      source: 'manual',
      observed_ms: Math.max(now, (old?.completion?.observed_ms || 0) + 1),
    },
  };
  pending.set(key, edit);
  return edit;
}

// setEpisodeCompleted sends the edit and resolves once acknowledged.
export async function setEpisodeCompleted(ep, completed) {
  const edit = completionEdit(ep, completed);
  const res = await API.syncPodcast({ episodes: [edit] });
  const editId = edit.completion.edit_id;
  const key = episodePendingKey(ep.feed_key, ep.episode_key);
  const rejected = (res?.rejected || []).find((r) => r.edit_id === editId);
  if (pending.get(key) === edit && (rejected || (res?.acked || []).includes(editId))) {
    pending.delete(key);
  }
  if (rejected) throw new Error(`Completion change rejected (${rejected.reason})`);
  if (!(res?.acked || []).includes(editId)) throw new Error('Completion change not confirmed');
  return res;
}

// pendingCompletionEdit exposes a pending edit (tests).
export function pendingCompletionEdit(feedKey, episodeKey) {
  return pending.get(episodePendingKey(feedKey, episodeKey));
}
