import assert from 'node:assert/strict';
import { readLibraryURL, libraryURL, safeReturnTo, loginDestination } from '../navigation-url.js';
const state = {
  selectedSetIds: [6], folderPath: 'R&B/Beyoncé/Space + # ? % & /日本語',
  virtualSet: '', mediaPage: 3,
  filters: { search: 'name & tags', favorites: true, tags: 'r&b', type: 'audio', sort: 'date', minDuration: '3.5' },
};
const url = libraryURL(state, 1973, true);
const route = readLibraryURL(url.slice(1));
assert.deepEqual(route.sets, [6]);
assert.equal(route.folder, state.folderPath);
assert.equal(route.media, 1973);
assert.equal(route.filters.search, state.filters.search);
assert.equal(route.filters.favorites, true);
assert.equal(route.page, 3);
assert.equal(route.shuffle, true);
for (const bad of ['0', '-1', '1e2', '1.2', '9007199254740992', '1/stream']) assert.equal(readLibraryURL('?media=' + bad).media, null);
for (const folder of ['../secret', 'a/../secret', '/etc', 'a\\b', 'a\0b']) assert.equal(readLibraryURL('?set=1&folder=' + encodeURIComponent(folder)).folder, '');
assert.deepEqual(readLibraryURL('?set=1,2,2,invalid').sets, [1, 2]);
assert.equal(readLibraryURL('?set=1,2&folder=a').folder, '');
assert.equal(readLibraryURL('?page=90071992547409999').page, undefined);
for (const bad of ['https://evil.example', '//evil.example', '/\\evil.example', '/login.html', '/api/media/1/stream', '/%2f%2fevil', '/index.html/../login.html']) assert.equal(safeReturnTo(bad), '/');
assert.equal(safeReturnTo(url), url);
assert.equal(new URLSearchParams(loginDestination('/login.html', url).split('?')[1]).get('return_to'), url);
console.log('Deep link serialization and return destination tests passed.');
const music = { ...state, selectedSetIds: [], folderPath: '', virtualSet: '', music: {album:'al_123',artist:'ar_456',name:'A & B / 東京'} };
assert.deepEqual(readLibraryURL(libraryURL(music).slice(1)).music, music.music);
assert.equal(readLibraryURL('?view=in-progress').music, null);

assert.match(libraryURL(music), /music=album/);
assert.equal(new URLSearchParams(libraryURL(music).slice(2)).has('search'), false);
assert.equal(readLibraryURL('?music=albums&search=ignored&set=1').filters.search, '');
assert.deepEqual(readLibraryURL('?music=albums&set=1').sets, []);
const artistList = {...music,music:{artists:true,artist:'',album:'',role:'composer'}};
const decodedArtists = readLibraryURL(libraryURL(artistList).slice(1)).music;
assert.equal(decodedArtists.artists,true);
assert.equal(decodedArtists.role,'composer');
assert.equal(decodedArtists.artist,'');
const artistAlbum = {...music,music:{artists:true,artist:'ar_person',album:'al_release',role:'performer'}};
assert.equal(readLibraryURL(libraryURL(artistAlbum).slice(1)).music.artist,'ar_person');
assert.equal(readLibraryURL(libraryURL(artistAlbum).slice(1)).music.album,'al_release');

for (const music of [{playlists:true,playlist:''},{playlists:true,playlist:'pl_abc'}]) {
 const encoded=libraryURL({...state,music});
 const decoded=readLibraryURL(encoded.slice(1)).music;
 assert.equal(decoded.playlists,true);
 assert.equal(decoded.playlist,music.playlist);
 assert.deepEqual(readLibraryURL(encoded.slice(1)).sets,[]);
}

for(const collection of ['recently_added','recently_played','most_played','random','favorites','top_rated']) {
 const encoded=libraryURL({...state,music:{album:'',collection}});
 assert.equal(readLibraryURL(encoded.slice(1)).music.collection,collection);
 assert.equal(readLibraryURL(`?music=album&id=al_a&mcollection=${collection}`).music.album,'al_a');
 assert.equal(readLibraryURL(`?music=album&id=al_a&mcollection=${collection}`).music.collection,collection);
 for(const route of ['artists','artist','playlists','playlist','search']) assert.equal(readLibraryURL(`?music=${route}&mcollection=${collection}`).music.collection,undefined);
}
assert.equal(readLibraryURL('?music=albums&mcollection=unknown').music.collection,undefined);
assert.equal(readLibraryURL('?music=album&id=al_a&mfrom=artists&mcollection=random').music.collection,undefined);
