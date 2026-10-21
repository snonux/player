// podcast-subscriptions.js — the caller's podcast subscription registers.
//
// Subscribing and unsubscribing are edits of the user's own register,
// sent to POST /api/podcasts/sync (docs/podcast-sync-contract.md §6.2).
// Unsubscribing writes a tombstone: it never deletes server files, catalog
// rows or listening history. Deleting a feed's server copies is a separate
// admin action ("Remove from server", DELETE /api/podcasts/{id}).
//
// An edit keeps its edit_id and clock until the server acknowledges it, so
// retrying a failed request (the same button again) is a replay the server
// treats as a no-op, not a new edit. Pending edits live in memory only: the
// web UI needs the server anyway.
import { API } from './api.js';

const pending = new Map(); // feed_key -> pending edit

// randomEditId returns 22 base64url characters (128 random bits).
export function randomEditId() {
  const bytes = new Uint8Array(16);
  globalThis.crypto.getRandomValues(bytes);
  let bin = '';
  bytes.forEach((b) => { bin += String.fromCharCode(b); });
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

// subscriptionEdit returns the edit that sets feed's register to
// subscribed, reusing a pending edit with the same value. URLs are only sent
// for canonical feed keys: a legacy key's stored URL has no identity and
// the server would reject it.
export function subscriptionEdit(feed, subscribed, now = Date.now()) {
  const old = pending.get(feed.feed_key);
  if (old && old.subscribed === subscribed) return old;
  const canonical = String(feed.feed_key || '').startsWith('feed:');
  const edit = {
    edit_id: randomEditId(),
    feed_key: feed.feed_key,
    title: feed.title || '',
    fetch_url: canonical ? feed.feed_url : '',
    urls: canonical && feed.feed_url ? [feed.feed_url] : [],
    subscribed,
    // Successive edits of one register are strictly increasing.
    observed_ms: Math.max(now, (old?.observed_ms || 0) + 1),
  };
  pending.set(feed.feed_key, edit);
  return edit;
}

// setSubscribed sends the edit and resolves once the server acknowledged
// it. A rejected edit can never succeed: it is dropped and reported.
export async function setSubscribed(feed, subscribed) {
  const edit = subscriptionEdit(feed, subscribed);
  const res = await API.syncPodcastSubscriptions([edit]);
  const rejected = (res?.rejected || []).find((r) => r.edit_id === edit.edit_id);
  if (pending.get(feed.feed_key) === edit && (rejected || (res?.acked || []).includes(edit.edit_id))) {
    pending.delete(feed.feed_key);
  }
  if (rejected) throw new Error(`Subscription change rejected (${rejected.reason})`);
  if (!(res?.acked || []).includes(edit.edit_id)) throw new Error('Subscription change not confirmed');
  return res;
}

// pendingEdit exposes the pending edit of a feed key (tests).
export function pendingEdit(feedKey) {
  return pending.get(feedKey);
}
