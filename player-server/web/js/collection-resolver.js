// Resolve whole batches before touching the client-owned queue. API list order
// is authoritative; user click order never changes collection ordering.
export async function resolveCollections(api, collections, current = () => true) {
 const result = [];
 const check = () => { if (!current()) throw new Error('Collection action cancelled.'); };
 for (const {kind, key} of collections) {
  check();
  if (kind === 'album' || kind === 'playlist') {
   const detail = kind === 'album' ? await api.album(key) : await api.playlist(key);
   check(); result.push(...detail.songs);
  } else if (kind === 'artist') {
   const artist = await api.artist(key); check();
   const seen = new Set();
   for (const album of artist.albums) {
    const detail = await api.album(album.album_key); check();
    for (const song of detail.songs) {
     if (!seen.has(song.id) && detail.song_credits?.[song.id]?.some(person => person.artist_key === key)) {
      seen.add(song.id); result.push(song);
     }
    }
   }
  } else throw new Error('Unknown collection.');
 }
 return result;
}
