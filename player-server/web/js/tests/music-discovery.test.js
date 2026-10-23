import assert from 'node:assert/strict';
globalThis.window={addEventListener(){}};
const { discoverAlbums } = await import('../music-discovery.js');
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
console.log('Album discovery: personal sources, exclusion, order, random membership and immutable catalog passed.');
