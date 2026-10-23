// Fisher–Yates keeps each occurrence, including repeated media, independent.
export function shuffledCopy(values, random = Math.random) {
  const result = [...values];
  for (let i = result.length - 1; i > 0; i--) {
    const j = Math.floor(random() * (i + 1));
    [result[i], result[j]] = [result[j], result[i]];
  }
  return result;
}
export function orderRemainder(entries, index, canonical, pinned, shuffle, random = Math.random, albums = {}) {
  const remaining = entries.slice(index + 1), next = remaining.filter(e => pinned.has(e.id));
  const rest = remaining.filter(e => !pinned.has(e.id));
  const rank = new Map(canonical.map((id, i) => [id, i]));
  const ordered = shuffle === 'albums' ? shuffledAlbums(rest, albums, random, entries[index]) : shuffle ? shuffledCopy(rest, random) : rest.sort((a, b) => rank.get(a.id) - rank.get(b.id));
  return [...entries.slice(0, index + 1), ...next, ...ordered];
}
export function parseShuffleOptions({genre = '', from = '', to = '', maximum = ''} = {}) {
  const number = (value, max, name) => {
    const text = String(value).trim();
    if (!text) return null;
    if (!/^\d+$/.test(text) || Number(text) < 1 || Number(text) > max) throw new Error(`${name} must be a whole number from 1 to ${max}.`);
    return Number(text);
  };
  const result = {genre: genre.trim().toLowerCase(), from: number(from, 9999, 'From year'), to: number(to, 9999, 'To year'), maximum: number(maximum, 10000, 'Maximum songs')};
  if (result.from && result.to && result.from > result.to) throw new Error('From year must not be after To year.');
  return result;
}
export function selectShuffleSongs(songs, options = {}, random = Math.random) {
  const eligible = songs.filter(song => song.type === 'audio' &&
    (!options.genre || song.music?.genres?.some(g => g.trim().toLowerCase() === options.genre)) &&
    (!options.from || song.music?.year >= options.from) &&
    (!options.to || (song.music?.year > 0 && song.music.year <= options.to)));
  if (!options.maximum || eligible.length <= options.maximum) return eligible;
  // Sample occurrence indexes, then retain chosen original order as canonical.
  const selected = new Set(shuffledCopy(eligible.map((_, i) => i), random).slice(0, options.maximum));
  return eligible.filter((_, i) => selected.has(i));
}

// Canonical album identities and ranks come from the permission-aware catalog.
// Missing entries are separate blocks, never an invented Unknown album.
export function shuffledAlbums(entries, albums, random = Math.random, current = null) {
  const groups = new Map();
  for (const entry of entries) {
    const album = albums[entry.media.id];
    const key = album?.album_key || entry;
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(entry);
  }
  for (const group of groups.values()) group.sort((a, b) => albums[a.media.id]?.order - albums[b.media.id]?.order || 0);
  const currentKey = albums[current?.media.id]?.album_key;
  const continuation = groups.get(currentKey) || [];
  groups.delete(currentKey);
  return [...continuation, ...shuffledCopy([...groups.values()], random).flat()];
}
