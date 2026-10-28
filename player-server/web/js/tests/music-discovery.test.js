import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
globalThis.window={addEventListener(){}};
const { discoverAlbums } = await import('../music-discovery.js');
const { collectionButtons } = await import('../collection-actions.js');
const albums=[
 {album_key:'a',title:'A',added_at:'2020-01-01',last_played_at:'2020-01-01',personal_play_count:9,rating:2},
 {album_key:'b',title:'B',added_at:'2025-01-01',last_played_at:'2025-01-01',personal_play_count:2,rating:5,is_favorite:true},
 {album_key:'c',title:'C',added_at:'2020-01-01',play_count:1000},
];
for(const [collection,expected] of Object.entries({recently_added:['b','a','c'],recently_played:['b','a'],most_played:['a','b'],favorites:['b'],liked_mix:['b'],rediscover:['a','b'],top_rated:['b','a']})) assert.deepEqual(discoverAlbums(albums,collection).map(a=>a.album_key),expected,collection);
const random=discoverAlbums(albums,'random',()=>0);
assert.deepEqual(random.map(a=>a.album_key),['b','c','a'],'zero RNG must perform the known Fisher–Yates permutation');
assert.deepEqual(albums.map(a=>a.album_key),['a','b','c'],'source order preserved');
assert.deepEqual(discoverAlbums([],'top_rated'),[]);
const discoverySource=readFileSync(fileURLToPath(new URL('../music-discovery.js', import.meta.url)),'utf8');
assert(!discoverySource.includes('button:not([data-collection-play])'),'discovery must not hide Shuffle/More behind Play-only chrome');
assert(discoverySource.includes('[data-collection-shuffle], [data-collection-menu]'),'empty discovery disables Shuffle and More only');
const chrome=collectionButtons('Play all 2 albums');
assert.match(chrome,/data-collection-shuffle/);
assert.match(chrome,/data-collection-menu[^>]*>☰/);
assert.match(chrome,/data-collection-download hidden/);
assert.match(chrome,/data-collection-pin hidden/);
assert.match(chrome,/data-collection-share hidden/);
console.log('Album discovery: personal sources, exclusion, order, random membership and immutable catalog passed.');
