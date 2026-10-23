import { mediaTitle } from './music.js';
const artist = media => media.music?.artist || media.music?.artists?.join(', ') || '';
export function offlineMusicGroups(songs, collections, kind) {
 if (kind === 'Playlists') return collections.filter(row => row.collection.kind === 'playlist').map(row => ({title:row.title,songs:row.songs,total:row.urls.length}));
 if (kind === 'Songs') return songs.map(song => ({title:mediaTitle(song.media),songs:[song],total:1}));
 const groups = new Map();
 for (const song of songs) {
  const m=song.media.music || {}, title=kind === 'Artists' ? artist(song.media) || 'Unknown artist' : m.album || 'Unknown album';
  const key = kind === 'Albums' ? JSON.stringify([title,m.album_artist || artist(song.media)]) : title;
  if (!groups.has(key)) groups.set(key,{title,songs:[],total:0});
  const group=groups.get(key);group.songs.push(song);group.total++;
 }
 for (const group of groups.values()) group.songs.sort((a,b) => (a.media.music?.disc || 0)-(b.media.music?.disc || 0) || (a.media.music?.track || 0)-(b.media.music?.track || 0) || mediaTitle(a.media).localeCompare(mediaTitle(b.media)));
 return [...groups.values()].sort((a,b)=>a.title.localeCompare(b.title));
}
export function offlineMusicMatches(group, query) {
 const text=[group.title,...group.songs.flatMap(song=>[mediaTitle(song.media),artist(song.media),song.media.music?.album || ''])].join(' ').toLocaleLowerCase();
 return query.toLocaleLowerCase().split(/\s+/).every(term=>text.includes(term));
}
