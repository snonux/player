// Each observation has its own durable key. Tabs can write concurrently without
// replacing another tab's unrelated deletion, or a true tombstone at a clock tie.
// localStorage already separates server origins; account IDs separate listeners.
const prefix = 'player-podcast-deletions:v1:';
export const podcastDeletionStorageKey = account => `${prefix}${account}`;
const identity = row => `${row.feed_key}\0${row.episode_key}`;
const validIdentity = key => typeof key === 'string' && key.split('\0').length === 2 && key.split('\0').every(Boolean);
const aliasRecord = row => validIdentity(row?.alias) && validIdentity(row?.canonical);
const ackRecord = row => typeof row?.acknowledged_edit_id === 'string' && row.acknowledged_edit_id.length >= 16;

function valid(row) {
  const value = row?.deletion;
  return typeof row?.feed_key === 'string' && row.feed_key.length > 0 &&
    typeof row.episode_key === 'string' && row.episode_key.length > 0 &&
    typeof value?.deleted === 'boolean' && Number.isSafeInteger(value.observed_ms) && value.observed_ms > 0 &&
    (row.saved_ms === undefined || (Number.isSafeInteger(row.saved_ms) && row.saved_ms > 0)) &&
    (!row.pending || (typeof row.pending.edit_id === 'string' && row.pending.edit_id.length >= 16 &&
      row.pending.deleted === value.deleted && row.pending.observed_ms === value.observed_ms));
}

function keys(account) {
  const beginning = `${podcastDeletionStorageKey(account)}:`;
  return Array.from({ length: localStorage.length }, (_, i) => localStorage.key(i)).filter(key => key?.startsWith(beginning));
}

function observation(key) {
  const encoded = localStorage.getItem(key);
  if (encoded === null) return null; // An alias may have been retired by another tab.
  const value = JSON.parse(encoded);
  if (value?.version !== 1 || (!valid(value) && !aliasRecord(value) && !ackRecord(value))) throw new Error('Saved podcast deletions could not be read');
  return value;
}

function wins(incoming, current) {
  if (!current || incoming.deletion.observed_ms !== current.deletion.observed_ms) {
    return !current || incoming.deletion.observed_ms > current.deletion.observed_ms;
  }
  if (incoming.deletion.deleted !== current.deletion.deleted) return incoming.deletion.deleted;
  // Later journal observations distinguish acknowledgement from a fresh epoch
  // replay at the same register clock. Equal journal clocks prefer a clean ack.
  if ((incoming.saved_ms || 0) !== (current.saved_ms || 0)) return (incoming.saved_ms || 0) > (current.saved_ms || 0);
  if (Boolean(incoming.pending) !== Boolean(current.pending)) return !incoming.pending;
  return (incoming.pending?.edit_id || '') > (current.pending?.edit_id || '');
}

export function readPodcastDeletions(account) {
  if (!account || !globalThis.localStorage) return [];
  const merged = new Map();
  const records = keys(account).map(observation).filter(Boolean);
  const acknowledged = new Set(records.filter(ackRecord).map(row => row.acknowledged_edit_id));
  const aliases = new Map(records.filter(aliasRecord).map(row => [row.alias, row.canonical]));
  const canonical = key => {
    const seen = new Set();
    while (aliases.has(key)) {
      if (seen.has(key)) throw new Error('Saved podcast aliases could not be read');
      seen.add(key); key = aliases.get(key);
    }
    return key;
  };
  const accept = original => {
    if (original.pending && acknowledged.has(original.pending.edit_id)) return;
    const key = canonical(identity(original));
    const [feed_key, episode_key] = key.split('\0');
    const row = { ...original, feed_key, episode_key };
    if (wins(row, merged.get(key))) merged.set(key, row);
  };
  // Accept the initial snapshot format so an interrupted client update does
  // not lose any saved edits. New writes only append journal observations.
  const legacy = JSON.parse(localStorage.getItem(podcastDeletionStorageKey(account)) || 'null');
  if (legacy) {
    if (legacy.version !== 1 || !Array.isArray(legacy.episodes) || !legacy.episodes.every(valid)) throw new Error('Saved podcast deletions could not be read');
    legacy.episodes.forEach(accept);
  }
  for (const row of records) if (!aliasRecord(row) && !ackRecord(row)) accept(row);
  return [...merged.values()];
}

export function podcastDeletionAcknowledged(account, editId) {
  if (!account || !editId || !globalThis.localStorage) return false;
  return observation(`${podcastDeletionStorageKey(account)}:ack:${editId}`)?.acknowledged_edit_id === editId;
}

export function writePodcastDeletions(account, rows, acceptedIds = []) {
  if (!account) throw new Error('Sign in to delete podcast episodes');
  const episodes = [...new Set(rows.values())].filter(row => row.deletion?.observed_ms > 0).map(row => ({
    feed_key: row.feed_key, episode_key: row.episode_key,
    deletion: { deleted: row.deletion.deleted, observed_ms: row.deletion.observed_ms },
    ...(row.pending.deletion ? { pending: { ...row.pending.deletion } } : {}),
  }));
  if (!episodes.length) return;
  if (!globalThis.localStorage) throw new Error('Browser storage is unavailable. Episode was not deleted.');
  readPodcastDeletions(account); // Validate saved data before writing any new observation.
  const records = keys(account).map(observation).filter(Boolean);
  const accepted = new Set([...acceptedIds, ...records.filter(ackRecord).map(row => row.acknowledged_edit_id)]);
  const existing = records.filter(row => !aliasRecord(row) && !ackRecord(row) && !accepted.has(row.pending?.edit_id));
  for (const row of episodes) {
    const value = row.deletion;
    const prior = existing.filter(item => identity(item) === identity(row));
    const latest = prior.reduce((best, item) => wins(item, best) ? item : best, null);
    if (latest) {
      if (latest.deletion.observed_ms > value.observed_ms ||
          (latest.deletion.observed_ms === value.observed_ms && latest.deletion.deleted && !value.deleted)) continue;
      const sameValue = latest.deletion.observed_ms === value.observed_ms && latest.deletion.deleted === value.deleted;
      if (sameValue && latest.pending?.edit_id === row.pending?.edit_id) continue;
      // A stale tab cannot requeue an edit whose exact value was already
      // confirmed. Epoch recovery creates a fresh edit identity instead.
      if (sameValue && !latest.pending && row.pending && prior.some(item => item.pending?.edit_id === row.pending.edit_id)) continue;
    }
    const saved_ms = Math.max(Date.now(), ...prior.map(item => (item.saved_ms || 0) + 1));
    const key = `${podcastDeletionStorageKey(account)}:${encodeURIComponent(row.feed_key)}:${encodeURIComponent(row.episode_key)}:${value.observed_ms}:${value.deleted ? 1 : 0}:${row.pending?.edit_id || 'server'}:${saved_ms}`;
    localStorage.setItem(key, JSON.stringify({ version: 1, ...row, saved_ms }));
  }
  // Keep immutable alias mappings as well as observations. A stale tab may
  // append a newer deletion under an old key while this tab saves its canonical
  // row; resolving on read preserves that deletion without deleting any key.
  for (const [alias, row] of rows) {
    const canonical = identity(row);
    if (alias === canonical) continue;
    const key = `${podcastDeletionStorageKey(account)}:alias:${encodeURIComponent(alias)}:${encodeURIComponent(canonical)}`;
    if (localStorage.getItem(key) === null) localStorage.setItem(key, JSON.stringify({ version: 1, alias, canonical }));
  }
  // Save the server observation before its receipt. A clamped server clock
  // may be lower than the device clock; the exact acknowledged edit must no
  // longer compete in the merge. A failed receipt write only causes a replay.
  for (const id of acceptedIds) {
    const key = `${podcastDeletionStorageKey(account)}:ack:${id}`;
    if (localStorage.getItem(key) === null) localStorage.setItem(key, JSON.stringify({ version: 1, acknowledged_edit_id: id }));
  }
}
