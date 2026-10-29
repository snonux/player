// Stable episode registers shared by cards and authenticated playback. Failed
// position/completion edits live in memory; deletion registers and replay edits
// persist for this account and server origin across reloads.
import { API } from './api.js';
import { readPodcastDeletions, writePodcastDeletions, podcastDeletionStorageKey, podcastDeletionAcknowledged } from './podcast-deletion-store.js';
const REGISTERS = ['position', 'completion', 'deletion'];
function wins(name, incoming, current) {
  if (!incoming) return false;
  const clock = incoming.observed_ms || 0, old = current?.observed_ms || 0;
  if (clock !== old) return clock > old;
  return name !== 'deletion' || clock <= 0 || !current?.deleted || incoming.deleted;
}
const persistDeletions = (acceptedIds = []) => writePodcastDeletions(account, rows, acceptedIds);
import { randomEditId, resetPodcastSubscriptions } from './podcast-subscriptions.js';

const rows = new Map();
let account = null, generation = 0, lastSuccess = null, lastError = null, lastFailure = null;
let epochRecovery = null;
const keyOf = ep => `${ep.feed_key}\0${ep.episode_key}`;
const changed = () => globalThis.document?.dispatchEvent(new Event('podcast:state-changed'));
const accountChannel = globalThis.window?.BroadcastChannel ? new window.BroadcastChannel('player-podcast-account') : null;
if (accountChannel) accountChannel.onmessage = event => setPodcastAccount(event.data?.account);

export function setPodcastAccount(id) {
  const next = Number.isSafeInteger(id) && id > 0 ? id : null;
  if (next === account) return;
  account = next;
  generation++;
  rows.clear();
  epochRecovery = null;
  resetPodcastSubscriptions();
  lastSuccess = lastError = lastFailure = null;
  if (account) {
    try {
      for (const saved of readPodcastDeletions(account)) {
        const row = rowFor(saved);
        if (saved.pending) row.pending.deletion = { ...saved.pending };
      }
    } catch (error) { lastError = error.message; lastFailure = error; }
  }
  accountChannel?.postMessage({ account });
  globalThis.document?.dispatchEvent(new Event('podcast:account-changed'));
  changed();
}

export function podcastSyncStatus() {
  const pendingIds = [...new Set(rows.values())].flatMap(r => Object.values(r.pending).map(edit => edit.edit_id));
  return { account, generation, pending: pendingIds.length, pendingIds, lastEpisodeSuccess: lastSuccess, lastError, lastFailure };
}

// Global pull is storage-only. Pending local registers win until acknowledged;
// aliases merge independently and never reach into live playback.
export function applyPodcastPull(response, lease = generation, { epochChanged = false } = {}) {
  if (lease !== generation || !account) return false;
  let updated = false;
  if (epochChanged && epochRecovery?.epoch !== response.epoch) {
    epochRecovery = { epoch: response.epoch, known: [...new Set(rows.values())].map(row => ({ row,
      position: row.position && { ...row.position }, completion: row.completion && { ...row.completion }, deletion: row.deletion && { ...row.deletion } })), pulled: new Map() };
  }
  for (const mapping of [...(response.resolved || []), ...(response.aliases || [])]) {
    for (const row of [...new Set(rows.values())]) {
      if (mapping.kind === 'feed' && row.feed_key === mapping.from) row.feed_key = mapping.to;
      else if (mapping.kind === 'episode' && row.feed_key === mapping.feed_key && row.episode_key === mapping.from) row.episode_key = mapping.to;
      else continue;
      updated = true;
      const target = rows.get(keyOf(row));
      if (target && target !== row) {
        for (const name of REGISTERS) {
          const hadPending = row.pending[name] || target.pending[name];
          if (wins(name, target[name], row[name])) {
            row[name] = target[name];
            if (target.pending[name]) row.pending[name] = target.pending[name]; else delete row.pending[name];
          }
          if (hadPending && row[name] && !row.pending[name]) {
            row.pending[name] = { ...row[name], edit_id: randomEditId() };
          }
        }
        for (const [key, value] of rows) if (value === target) rows.set(key, row);
      }
      rows.set(keyOf(row), row);
    }
  }
  for (const episode of response.episodes || []) {
    const before = rows.get(keyOf(episode));
    const encoded = JSON.stringify([before?.position, before?.completion, before?.deletion]);
    const after = rowFor(episode);
    if (encoded !== JSON.stringify([after.position, after.completion, after.deletion])) updated = true;
  }
  if (epochRecovery) for (const episode of response.episodes || []) epochRecovery.pulled.set(keyOf(episode), episode);
  if (updated) { persistDeletions(); changed(); }
  return true;
}

// Like native epoch recovery, replay known clean values missing from a restored
// server with their original clocks. Interrupted pulls retain markers in this
// account's tab memory; there is no new browser history store.
export function finishPodcastPull(lease = generation) {
  if (lease !== generation || !account || !epochRecovery) return;
  const live = new Set(rows.values());
  for (const original of epochRecovery.known) {
    const row = original.row;
    if (!live.has(row)) continue;
    const remote = epochRecovery.pulled.get(keyOf(row));
    for (const name of REGISTERS) {
      if (original[name] && !row.pending[name] &&
          ((remote?.[name]?.observed_ms || 0) < original[name].observed_ms ||
           (name === 'deletion' && original[name].deleted && remote?.deletion?.deleted === false &&
             remote.deletion.observed_ms === original[name].observed_ms))) {
        row.pending[name] = { ...row[name], edit_id: randomEditId() };
      }
    }
  }
  epochRecovery = null;
  persistDeletions();
  changed();
}

function rowFor(ep) {
  const key = keyOf(ep);
  if (!rows.has(key)) rows.set(key, { feed_key: ep.feed_key, episode_key: ep.episode_key, pending: {}, position: null, completion: null, deletion: null });
  const row = rows.get(key);
  for (const name of REGISTERS) {
    const value = ep[name];
    if (value && !row.pending[name] && wins(name, value, row[name])) row[name] = { ...value };
  }
  // Listing positions have no clocks. Use only before a register is known.
  if (!row.position && ep.position_seconds > 0) row.position = { seconds: ep.position_seconds, observed_ms: 0 };
  return row;
}

export function episodeSnapshot(ep) {
  const row = rowFor(ep);
  return { feed_key: row.feed_key, episode_key: row.episode_key,
    position: row.position && { ...row.position }, completion: row.completion && { ...row.completion }, deletion: row.deletion && { ...row.deletion } };
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
    let capabilityProblem = null;
    if (sent.deletion) {
      const config = await API.syncConfig();
      if (owner !== generation) return null;
      if (config.user_id !== account) throw new Error('Account changed');
      if (config.podcast_episode_deletion !== true) {
        delete sent.deletion;
        capabilityProblem = new Error('Update this server to sync episode deletions. Your deletion is saved on this browser.');
      }
    }
    const res = await API.syncPodcast({ scope, episodes: Object.keys(sent).length ? [{ ...scope, ...sent }] : [] });
    if (owner !== generation) return null;
    const returned = (res?.episodes || []).find(r => r.feed_key === scope.feed_key && r.episode_key === scope.episode_key)
      || resolvedEpisode(res, scope);
    const deletionAcks = sent.deletion && (res.acked || []).includes(sent.deletion.edit_id) ? [sent.deletion.edit_id] : [];
    let problem = capabilityProblem;
    for (const name of REGISTERS) {
      const edit = sent[name];
      const rejected = edit && (res.rejected || []).find(r => r.edit_id === edit.edit_id);
      const acked = edit && (res.acked || []).includes(edit.edit_id);
      if (edit && row.pending[name]?.edit_id === edit.edit_id && (acked || (rejected && name !== 'deletion'))) {
        delete row.pending[name];
        if (acked && returned?.[name]) row[name] = { ...returned[name] };
      }
      if (rejected || (edit && !acked)) problem = Object.assign(new Error(rejected ? `Podcast change rejected (${rejected.reason})` : 'Podcast change not confirmed'), { kind: rejected ? 'rejected' : 'server' });
      if (returned?.[name] && !row.pending[name] && wins(name, returned[name], row[name])) row[name] = { ...returned[name] };
    }
    if (returned) {
      const other = rows.get(keyOf(returned));
      if (other && other !== row) {
        for (const name of REGISTERS) {
          if (wins(name, other[name], row[name])) {
            row[name] = other[name];
            if (other.pending[name]) row.pending[name] = other.pending[name];
            else delete row.pending[name];
          }
        }
        for (const [key, value] of rows) if (value === other) rows.set(key, row);
      }
      row.feed_key = returned.feed_key; row.episode_key = returned.episode_key; rows.set(keyOf(row), row);
    }
    persistDeletions(deletionAcks);
    if (problem) throw problem;
    lastError = lastFailure = null;
    lastSuccess = Date.now();
    changed();
    return episodeSnapshot(row);
  } catch (error) {
    if (owner === generation) { lastError = error.message; lastFailure = error; changed(); }
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

// Save the tombstone before notifying the view or starting network work.
export function deletionEdit(ep, deleted = true, now = Date.now()) {
  if (!account || (ep.podcast_generation !== undefined && ep.podcast_generation !== generation)) throw new Error('Account changed. Reopen podcasts.');
  const row = rowFor(ep), previous = row.deletion, pending = row.pending.deletion;
  if (pending?.deleted !== Boolean(deleted)) {
    row.deletion = row.pending.deletion = { edit_id: randomEditId(), deleted: Boolean(deleted),
      observed_ms: Math.max(now, (row.deletion?.observed_ms || 0) + 1) };
    try { persistDeletions(); }
    catch (error) { row.deletion = previous; if (pending) row.pending.deletion = pending; else delete row.pending.deletion; throw error; }
    changed();
  }
  return pendingItem(row);
}

export async function setEpisodeDeleted(ep, deleted = true) {
  deletionEdit(ep, deleted);
  return syncEpisode(ep, true);
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
  const owner = generation;
  const unique = [...new Set(rows.values())];
  for (const row of unique) {
    if (owner !== generation) throw new Error('Account changed');
    if (Object.keys(row.pending).length) await syncEpisode(row);
  }
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

// Another tab may finish or queue the same account's deletion. Adopt newer
// tombstones without rewriting the storage event back to that tab.
globalThis.addEventListener?.('storage', event => {
  if (!account || !(event.key === podcastDeletionStorageKey(account) || event.key?.startsWith(`${podcastDeletionStorageKey(account)}:`))) return;
  try {
    for (const saved of readPodcastDeletions(account)) {
      const row = rowFor({ feed_key: saved.feed_key, episode_key: saved.episode_key });
      const acknowledged = podcastDeletionAcknowledged(account, row.pending.deletion?.edit_id);
      if (acknowledged || wins('deletion', saved.deletion, row.deletion)) {
        row.deletion = { ...saved.deletion };
        if (saved.pending) row.pending.deletion = { ...saved.pending }; else delete row.pending.deletion;
      }
    }
    changed();
  } catch (error) { lastError = error.message; lastFailure = error; changed(); }
});
