// A boundary commits an occurrence only while its owner and successor still
// match. Unrelated edits behind the current song need not interrupt playback.
export function preparedQueuePlan({ entries, index, scope, revision, repeat }) {
  if (!entries[index]) return null;
  let target = repeat === 'one' ? index : index + 1;
  if (target >= entries.length && repeat === 'all') target = 0;
  const entry = entries[target];
  if (!entry) return null;
  return { media: entry.media, target,
    token: JSON.stringify([scope, revision, entries[index].id, entry.id, target, repeat]) };
}
