import assert from 'node:assert/strict';
import { shuffledCopy, orderRemainder, parseShuffleOptions, selectShuffleSongs } from '../queue-shuffle.js';
const entries = [1,2,3,4,5,6].map(id => ({id, media:{id:id===6?3:id}}));
const canonical = entries.map(e=>e.id), pinned = new Set([3,4]);
const mixed = orderRemainder(entries,1,canonical,pinned,true,()=>0);
assert.deepEqual(mixed.slice(0,4),entries.slice(0,4));
assert.deepEqual(mixed.map(e=>e.id).sort((a,b)=>a-b),canonical);
assert.deepEqual(orderRemainder(mixed,2,canonical,pinned,false),entries);
assert.deepEqual(orderRemainder(mixed,4,canonical,pinned,false).slice(0,5),mixed.slice(0,5));
assert.deepEqual(shuffledCopy([1,2,3],()=>0),[2,3,1]);
const song=(id,year,genres)=>({id,type:'audio',music:{year,genres}});
const songs=[song(1,2021,[' Jazz ']),song(2,2022,['Jazz','Rock']),song(1,2021,['Jazz']),song(3,2020,['Rock']),{id:4,type:'audio'}];
const options=parseShuffleOptions({genre:' JAZZ ',from:'2021',to:'2022',maximum:'2'});
assert.deepEqual(selectShuffleSongs(songs,options,()=>0).map(s=>s.id),[2,1]);
assert.deepEqual(selectShuffleSongs(songs,parseShuffleOptions({genre:'jazz'})).map(s=>s.id),[1,2,1]);
assert.equal(selectShuffleSongs(songs,parseShuffleOptions({from:'2023'})).length,0);
assert.deepEqual(selectShuffleSongs(songs),songs);
for (const invalid of [{from:'0'},{to:'10000'},{from:'2022',to:'2021'},{maximum:'0'},{maximum:'10001'},{maximum:'1.2'},{maximum:'1e2'},{from:'abc'}]) assert.throws(()=>parseShuffleOptions(invalid));
console.log('Occurrence shuffle, pinned remainder and optional collection filters passed.');
// Album IDs/ranks are canonical server output: divergent compilation credits,
// discs and missing tags need no client identity heuristic.
const albumEntries = [1,2,3,4,5,6,7,8].map(id => ({id, media:{id}}));
const albumMap = {1:{album_key:'A',order:0},2:{album_key:'A',order:2},3:{album_key:'B',order:1},4:{album_key:'A',order:1},5:{album_key:'B',order:0},6:{album_key:'C',order:0}};
const albumMix = orderRemainder(albumEntries,0,albumEntries.map(e=>e.id),new Set([3]),'albums',()=>0,albumMap);
assert.deepEqual(albumMix.map(e=>e.id),[1,3,4,2,6,7,8,5]);
assert.deepEqual(albumMix.map(e=>e.id).sort((a,b)=>a-b),albumEntries.map(e=>e.id));
assert.deepEqual(orderRemainder(albumMix,0,albumEntries.map(e=>e.id),new Set([3]),false).map(e=>e.id),[1,3,2,4,5,6,7,8]);
const repeated = [{id:20,media:{id:5}}, {id:21,media:{id:3}}, {id:22,media:{id:5}}];
assert.deepEqual(orderRemainder(repeated,-1,[20,21,22],new Set(),'albums',()=>0,albumMap).map(e=>e.id),[20,22,21]);
