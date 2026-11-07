// Retention measures device observation time, never a potentially old report's
// clock. These account-scoped markers contain no position or listening history.
export const podcastRetentionMS = 7 * 24 * 60 * 60 * 1000;
const prefix = 'player-podcast-retention:v1:';
const key = ep => `${encodeURIComponent(ep.feed_key)}:${encodeURIComponent(ep.episode_key)}`;
const storageKey = (account, ep) => `${prefix}${account}:${key(ep)}`;

export function readPodcastRetention(account) {
  if (!account || !globalThis.localStorage) return [];
  const start = `${prefix}${account}:`, result = [];
  for (let i = 0; i < localStorage.length; i++) {
    const name = localStorage.key(i);
    if (!name?.startsWith(start)) continue;
    const row = JSON.parse(localStorage.getItem(name));
    if (typeof row?.feed_key !== 'string' || typeof row?.episode_key !== 'string' ||
        !Number.isSafeInteger(row.completion_ms) || row.completion_ms <= 0 ||
        !Number.isSafeInteger(row.seen_ms) || row.seen_ms <= 0) throw Error('Saved podcast retention could not be read');
    result.push(row);
  }
  return result;
}

export function observePodcastRetention(account, ep, now = Date.now()) {
  if (!account || !globalThis.localStorage) return null;
  const name = storageKey(account, ep);
  const old = JSON.parse(localStorage.getItem(name) || 'null');
  const completion = ep.completion;
  if (!old && !(completion?.observed_ms > 0)) return null;
  const newer = completion?.observed_ms > (old?.completion_ms || 0);
  const row = newer ? { ...old, feed_key: ep.feed_key, episode_key: ep.episode_key,
    completion_ms: completion.observed_ms, completed: completion.completed, seen_ms: now } : { ...old };
  // Completion ties accept the authoritative merged register, just like the
  // episode store. Equal-clock corrections must not restart the grace.
  if (completion?.observed_ms === row.completion_ms) row.completed = completion.completed;
  row.position_ms = Math.max(row.position_ms || 0, ep.position?.observed_ms || 0);
  row.deletion_ms = Math.max(row.deletion_ms || 0, ep.deletion?.observed_ms || 0);
  if (ep.server_copy?.media_id) row.media_id = ep.server_copy.media_id;
  if (JSON.stringify(row) !== JSON.stringify(old)) localStorage.setItem(name, JSON.stringify(row));
  return row;
}

export function podcastRetentionExpired(row, now = Date.now()) {
  return Boolean(row?.completed && now >= row.seen_ms + podcastRetentionMS &&
    row.position_ms <= row.completion_ms && row.deletion_ms < row.completion_ms);
}

export function remapPodcastRetention(account, mappings) {
  for (const old of readPodcastRetention(account)) {
    const row = { ...old };
    for (const mapping of mappings) {
      if (mapping.kind === 'feed' && row.feed_key === mapping.from) row.feed_key = mapping.to;
      if (mapping.kind === 'episode' && row.feed_key === mapping.feed_key && row.episode_key === mapping.from) row.episode_key = mapping.to;
    }
    if (key(old) === key(row)) continue;
    const targetKey = storageKey(account, row);
    const existing = JSON.parse(localStorage.getItem(targetKey) || 'null');
    // Completion chooses its own clock/grace. Replay and restore are
    // independent monotonic guards, including when the other completion wins.
    const winner = !existing || row.completion_ms > existing.completion_ms ? row : existing;
    const merged = { ...winner,
      position_ms: Math.max(row.position_ms || 0, existing?.position_ms || 0),
      deletion_ms: Math.max(row.deletion_ms || 0, existing?.deletion_ms || 0) };
    if (row.completion_ms === existing?.completion_ms) merged.seen_ms = Math.min(row.seen_ms, existing.seen_ms);
    const mediaId = winner.media_id || row.media_id || existing?.media_id;
    if (mediaId) merged.media_id = mediaId;
    localStorage.setItem(targetKey, JSON.stringify(merged));
    localStorage.removeItem(storageKey(account, old));
  }
}
