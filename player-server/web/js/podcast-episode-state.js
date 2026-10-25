// Stable episode registers shared by cards and authenticated playback. Failed
// edits live in memory for this account; reloading requires the server again.
import { API } from './api.js';
import { randomEditId, resetPodcastSubscriptions } from './podcast-subscriptions.js';

const rows = new Map();
let account = null, generation = 0, lastSuccess = null, lastError = null;
const keyOf = ep => `${ep.feed_key}\0${ep.episode_key}`;
const changed = () => globalThis.document?.dispatchEvent(new Event('podcast:state-changed'));
const accountChannel = globalThis.window && globalThis.BroadcastChannel ? new BroadcastChannel('player-podcast-account') : null;
if (accountChannel) accountChannel.onmessage = event => setPodcastAccount(event.data?.account);

export function setPodcastAccount(id) {
  const next = Number.isSafeInteger(id) && id > 0 ? id : null;
  if (next === account) return;
  account = next;
  generation++;
  rows.clear();
  resetPodcastSubscriptions();
  lastSuccess = lastError = null;
  accountChannel?.postMessage({ account });
  globalThis.document?.dispatchEvent(new Event('podcast:account-changed'));
  changed();
}

export function podcastSyncStatus() {
  return { account, generation, pending: [...new Set(rows.values())].reduce((n, r) => n + Object.keys(r.pending).length, 0), lastEpisodeSuccess: lastSuccess, lastError };
}

function rowFor(ep) {
  const key = keyOf(ep);
  if (!rows.has(key)) rows.set(key, { feed_key: ep.feed_key, episode_key: ep.episode_key, pending: {}, position: null, completion: null });
  const row = rows.get(key);
  for (const name of ['position', 'completion']) {
    const value = ep[name];
    if (value && !row.pending[name] && value.observed_ms >= (row[name]?.observed_ms || 0)) row[name] = { ...value };
  }
  // Listing positions have no clocks. Use only before a register is known.
  if (!row.position && ep.position_seconds > 0) row.position = { seconds: ep.position_seconds, observed_ms: 0 };
  return row;
}

export function episodeSnapshot(ep) {
  const row = rowFor(ep);
  return { feed_key: row.feed_key, episode_key: row.episode_key,
    position: row.position && { ...row.position }, completion: row.completion && { ...row.completion } };
}

function pendingItem(row) {
  return { feed_key: row.feed_key, episode_key: row.episode_key, ...row.pending };
}

export function completionEdit(ep, completed, now = Date.now()) {
  if (ep.podcast_generation !== undefined && ep.podcast_generation !== generation) throw new Error('Account changed. Reopen podcasts.');
  const row = rowFor(ep);
  if (row.pending.completion?.completed !== Boolean(completed)) {
    row.pending.completion = { edit_id: randomEditId(), completed: Boolean(completed), source: 'manual',
      observed_ms: Math.max(now, (row.completion?.observed_ms || 0) + 1) };
    row.completion = { ...row.pending.completion };
    changed();
  }
  return pendingItem(row);
}

// Local end commit is synchronous and atomic to observers. Transport cannot
// hold the next episode. Manual completion remains independent of positions.
export function saveEpisodePosition(ep, seconds, { finished = false, now = Date.now(), lease = generation } = {}) {
  if (!Number.isFinite(seconds) || seconds < 0 || !account || lease !== generation) return null;
  const row = rowFor(ep);
  const ms = Math.max(now, (row.position?.observed_ms || 0) + 1,
    finished ? (row.completion?.observed_ms || 0) + 1 : 0);
  row.position = row.pending.position = { edit_id: randomEditId(), seconds, observed_ms: ms };
  if (finished) row.completion = row.pending.completion = {
    edit_id: randomEditId(), completed: true, source: 'auto', observed_ms: ms };
  changed();
  void syncEpisode(ep, finished).catch(() => {});
  return ms;
}

export function syncEpisode(ep, immediate = false) {
  if (!account) return Promise.reject(new Error('Sign in to sync podcasts'));
  const row = rowFor(ep);
  if (row.inflight && !immediate) return row.inflight;
  const request = sendEpisode(ep);
  row.inflight = request;
  const clear = () => { if (row.inflight === request) delete row.inflight; };
  void request.then(clear, clear);
  return request;
}

async function sendEpisode(ep) {
  if (!account) throw new Error('Sign in to sync podcasts');
  const owner = generation, row = rowFor(ep), sent = { ...row.pending };
  const scope = { feed_key: row.feed_key, episode_key: row.episode_key };
  try {
    const res = await API.syncPodcast({ scope, episodes: Object.keys(sent).length ? [{ ...scope, ...sent }] : [] });
    if (owner !== generation) return null;
    const returned = (res?.episodes || []).find(r => r.feed_key === scope.feed_key && r.episode_key === scope.episode_key)
      || resolvedEpisode(res, scope);
    let problem = null;
    for (const name of ['position', 'completion']) {
      const edit = sent[name];
      const rejected = edit && (res.rejected || []).find(r => r.edit_id === edit.edit_id);
      const acked = edit && (res.acked || []).includes(edit.edit_id);
      if (edit && row.pending[name]?.edit_id === edit.edit_id && (acked || rejected)) {
        delete row.pending[name];
        if (acked && returned?.[name]) row[name] = { ...returned[name] };
      }
      if (rejected || (edit && !acked)) problem = new Error(rejected ? `Podcast change rejected (${rejected.reason})` : 'Podcast change not confirmed');
      if (returned?.[name] && !row.pending[name] && returned[name].observed_ms >= (row[name]?.observed_ms || 0)) row[name] = { ...returned[name] };
    }
    if (returned) {
      const other = rows.get(keyOf(returned));
      if (other && other !== row) {
        for (const name of ['position', 'completion']) {
          if ((other[name]?.observed_ms || 0) >= (row[name]?.observed_ms || 0)) {
            row[name] = other[name];
            if (other.pending[name]) row.pending[name] = other.pending[name];
            else delete row.pending[name];
          }
        }
        for (const [key, value] of rows) if (value === other) rows.set(key, row);
      }
      row.feed_key = returned.feed_key; row.episode_key = returned.episode_key; rows.set(keyOf(row), row);
    }
    if (problem) throw problem;
    lastError = null;
    lastSuccess = Date.now();
    changed();
    return episodeSnapshot(row);
  } catch (error) {
    if (owner === generation) { lastError = error.message; changed(); }
    throw error;
  }
}

function resolvedEpisode(res, scope) {
  let feed = scope.feed_key, episode = scope.episode_key;
  for (const mapping of [...(res.resolved || []), ...(res.aliases || [])]) {
    if (mapping.kind === 'feed' && mapping.from === feed) feed = mapping.to;
    if (mapping.kind === 'episode' && mapping.feed_key === feed && mapping.from === episode) episode = mapping.to;
  }
  return (res.episodes || []).find(r => r.feed_key === feed && r.episode_key === episode);
}

export async function setEpisodeCompleted(ep, completed) {
  completionEdit(ep, completed);
  return syncEpisode(ep, true);
}

export function pendingCompletionEdit(feedKey, episodeKey) {
  const row = rows.get(keyOf({ feed_key: feedKey, episode_key: episodeKey }));
  return row?.pending.completion ? pendingItem(row) : undefined;
}

export async function retryPodcastEpisodeEdits() {
  const unique = [...new Set(rows.values())];
  for (const row of unique) await syncEpisode(row);
}

// One bounded attempt. Its continuation has no player reference.
export async function reconcileEpisode(ep, timeoutMS = 3000) {
  const owner = generation;
  let timer;
  try {
    await Promise.race([syncEpisode(ep).catch(() => null), new Promise(resolve => { timer = setTimeout(resolve, timeoutMS); })]);
  } finally { clearTimeout(timer); }
  return owner === generation ? episodeSnapshot(ep) : null;
}

export function podcastStartPosition(position, duration, fromBeginning = false) {
  const seconds = Number(position) || 0;
  return fromBeginning || (Number.isFinite(duration) && duration > 0 && seconds >= duration - 5) ? 0 : Math.max(0, seconds);
}

globalThis.addEventListener?.('online', () => { void retryPodcastEpisodeEdits().catch(() => {}); });
