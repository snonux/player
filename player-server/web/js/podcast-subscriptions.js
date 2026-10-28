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
let generation = 0, lastError = null;
const changed = () => globalThis.document?.dispatchEvent(new Event('podcast:state-changed'));

export function resetPodcastSubscriptions() { generation++; pending.clear(); lastError = null; }
export function podcastSubscriptionStatus() { return { lastError }; }

export function pendingPodcastSubscriptions() { return [...pending.values()]; }

export async function retryPodcastSubscriptions() {
  const owner = generation;
  for (const edit of pendingPodcastSubscriptions()) {
    if (owner !== generation) throw new Error('Account changed');
    await setSubscribed({ ...edit, feed_url: edit.fetch_url }, edit.subscribed);
  }
}

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
  changed();
  return edit;
}

// setSubscribed sends the edit and resolves once the server acknowledged
// it. A rejected edit can never succeed: it is dropped and reported.
export async function setSubscribed(feed, subscribed) {
  const edit = subscriptionEdit(feed, subscribed);
  const owner = generation;
  try {
  const res = await API.syncPodcastSubscriptions([edit]);
  if (owner !== generation) return null;
  const rejected = (res?.rejected || []).find((r) => r.edit_id === edit.edit_id);
  if (pending.get(feed.feed_key) === edit && (rejected || (res?.acked || []).includes(edit.edit_id))) {
    pending.delete(feed.feed_key);
  }
  if (rejected) throw Object.assign(new Error(`Subscription change rejected (${rejected.reason})`), { kind: 'rejected' });
  if (!(res?.acked || []).includes(edit.edit_id)) throw new Error('Subscription change not confirmed');
  const canonical = (res.resolved || []).find(r => r.kind === 'feed' && r.from === feed.feed_key)?.to || feed.feed_key;
  const current = (res.subscriptions || []).find(r => r.feed_key === canonical);
  if (current && current.subscribed !== subscribed) throw new Error('A newer subscription change was kept. Refresh to see it.');
  lastError = null; changed();
  return res;
  } catch (error) {
    if (owner === generation) { lastError = error; changed(); }
    throw error;
  }
}

// pendingEdit exposes the pending edit of a feed key (tests).
export function pendingEdit(feedKey) {
  return pending.get(feedKey);
}

// The catalog endpoint is permission-filtered server media. A subscription
// grants no set access, so ordinary listeners also need their own registers.
// Read every pull page without sending edits, retaining the account lease.
export async function listAccountPodcastSubscriptions(catalog = []) {
  const owner = generation;
  const registers = new Map();
  let epoch = '', since = 0;
  do {
    const page = await API.syncPodcast({ epoch, since, limit: 500 });
    if (owner !== generation) throw new Error('Account changed');
    if (!Array.isArray(page.subscriptions) || typeof page.epoch !== 'string' ||
        !Number.isSafeInteger(page.next_since) || typeof page.has_more !== 'boolean') {
      throw new Error('Could not read podcast subscriptions. Retry.');
    }
    if (epoch && page.epoch !== epoch) throw new Error('Podcast library changed. Retry.');
    for (const row of page.subscriptions) registers.set(row.feed_key, row);
    if (!page.has_more) break;
    if (page.epoch === epoch && page.next_since <= since) throw new Error('Could not finish reading podcast subscriptions. Retry.');
    epoch = page.epoch; since = page.next_since;
  } while (true);
  const byKey = new Map(catalog.map(feed => [feed.feed_key, feed]));
  return [...registers.values()].filter(row => row.subscribed).map(row => {
    const feed = byKey.get(row.feed_key) || {};
    return { ...feed, ...row, title: row.title || feed.title || row.fetch_url || row.feed_key,
      feed_url: row.fetch_url || feed.feed_url || '' };
  });
}
