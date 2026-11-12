// Unsent position and completion edits survive a reload or a closed tab, so
// listening done while the server was unreachable is sent once it is back.
// Deletions have their own journal (podcast-deletion-store.js). One record per
// episode; tabs merge by edit clock and drop only edits the server answered.
const prefix = 'player-podcast-pending:v1:';
const NAMES = ['position', 'completion'];
export const podcastPendingStorageKey = account => `${prefix}${account}`;
const identity = row => `${row.feed_key}\0${row.episode_key}`;

function validEdit(name, edit) {
  if (!edit || typeof edit.edit_id !== 'string' || edit.edit_id.length < 16 ||
      !Number.isSafeInteger(edit.observed_ms) || edit.observed_ms <= 0) return false;
  return name === 'position'
    ? Number.isFinite(edit.seconds) && edit.seconds >= 0
    : typeof edit.completed === 'boolean' && (edit.source === 'manual' || edit.source === 'auto');
}

function read(account) {
  const encoded = globalThis.localStorage?.getItem(podcastPendingStorageKey(account));
  if (!encoded) return {};
  const saved = JSON.parse(encoded);
  if (saved?.version !== 1 || typeof saved.episodes !== 'object' || !saved.episodes) return {};
  const episodes = {};
  for (const entry of Object.values(saved.episodes)) {
    if (typeof entry?.feed_key !== 'string' || !entry.feed_key || typeof entry.episode_key !== 'string' || !entry.episode_key) continue;
    const kept = { feed_key: entry.feed_key, episode_key: entry.episode_key };
    for (const name of NAMES) if (validEdit(name, entry[name])) kept[name] = { ...entry[name] };
    if (NAMES.some(name => kept[name])) episodes[identity(kept)] = kept;
  }
  return episodes;
}

/** Saved unsent edits of this account: [{feed_key, episode_key, position?, completion?}]. */
export function readPodcastPendingEdits(account) {
  if (!account) return [];
  try { return Object.values(read(account)); } catch { return []; }
}

/**
 * Saves the pending position/completion edits of rows. Edits saved by another
 * tab stay unless this tab holds a newer edit of the same register; `done`
 * lists edit IDs the server acknowledged or rejected, which are removed.
 */
export function writePodcastPendingEdits(account, rows, done = []) {
  if (!account || !globalThis.localStorage) return;
  try {
    const finished = new Set(done);
    let saved;
    try { saved = read(account); } catch { saved = {}; }
    const episodes = {};
    for (const [key, entry] of Object.entries(saved)) {
      for (const name of NAMES) {
        if (!entry[name] || finished.has(entry[name].edit_id)) continue;
        episodes[key] ??= { feed_key: entry.feed_key, episode_key: entry.episode_key };
        episodes[key][name] = entry[name];
      }
    }
    for (const row of new Set(rows.values())) {
      const key = identity(row);
      for (const name of NAMES) {
        const edit = row.pending?.[name];
        const current = episodes[key]?.[name];
        if (!edit || (current && current.observed_ms > edit.observed_ms)) continue;
        episodes[key] ??= { feed_key: row.feed_key, episode_key: row.episode_key };
        episodes[key][name] = { ...edit };
      }
    }
    for (const [key, entry] of Object.entries(episodes)) if (!NAMES.some(name => entry[name])) delete episodes[key];
    if (Object.keys(episodes).length) {
      localStorage.setItem(podcastPendingStorageKey(account), JSON.stringify({ version: 1, episodes }));
    } else {
      localStorage.removeItem(podcastPendingStorageKey(account));
    }
  } catch { /* Storage full or blocked: edits still retry while this tab is open. */ }
}
