// Only identifiers and queue settings are persisted, never source URLs or auth.
export const queueStorageKey = (origin, userId) => `player.queue.${origin}.${userId}`;
export function readQueueSnapshot(raw) {
  if (raw === null) return null;
  const value = JSON.parse(raw);
  if (value?.version !== 1 || !Array.isArray(value.entries) || value.entries.length > 10000 ||
      (value.repeat !== undefined && !['off', 'all', 'one'].includes(value.repeat)) ||
      !Number.isInteger(value.index) || value.index < -1 || value.index >= value.entries.length ||
      (value.entries.length > 0 && value.index < 0) ||
      !Number.isFinite(value.position) || value.position < 0 || typeof value.label !== 'string' ||
      value.label.length > 2000 || !Array.isArray(value.upNext)) throw new Error('Invalid queue');
  if (value.shuffle !== undefined && typeof value.shuffle !== 'boolean') throw new Error('Invalid shuffle');
  const ids = new Set();
  for (const entry of value.entries) {
    if (!Number.isSafeInteger(entry.id) || entry.id <= 0 || ids.has(entry.id) ||
        !Number.isSafeInteger(entry.mediaId) || entry.mediaId <= 0) throw new Error('Invalid queue entry');
    ids.add(entry.id);
  }
  if (value.canonical !== undefined && (!Array.isArray(value.canonical) || value.canonical.length !== ids.size || new Set(value.canonical).size !== ids.size || value.canonical.some(id => !ids.has(id)))) throw new Error('Invalid canonical order');
  if (value.shuffle === true && value.canonical === undefined) throw new Error('Missing canonical order');
  if (value.upNext.some(id => !ids.has(id))) throw new Error('Invalid next entry');
  return value;
}
