import assert from 'node:assert/strict';
import { resolveCollections } from '../collection-resolver.js';
const song = id => ({id,type:'audio'});
const api = {
 album: async key => ({songs:(key==='a'?[2,1]:[3,2]).map(song),song_credits:{1:[{artist_key:'artist'}],2:[{artist_key:'other'}],3:[{artist_key:'artist'}]}}),
 playlist: async () => ({songs:[1,2,1].map(song)}),
 artist: async () => ({albums:[{album_key:'a'},{album_key:'b'},{album_key:'a'}]})
};
const ids = values => values.map(song=>song.id);
assert.deepEqual(ids(await resolveCollections(api,[{kind:'album',key:'a'},{kind:'album',key:'b'}])),[2,1,3,2]);
assert.deepEqual(ids(await resolveCollections(api,[{kind:'artist',key:'artist'}])),[1,3]);
assert.deepEqual(ids(await resolveCollections(api,[{kind:'playlist',key:'p'},{kind:'playlist',key:'q'}])),[1,2,1,1,2,1]);
assert.deepEqual(ids(await resolveCollections(api,[{kind:'artist',key:'artist'},{kind:'album',key:'a'}])),[1,3,2,1]);
await assert.rejects(resolveCollections({...api,album:async key=>{if(key==='b')throw Error('offline');return api.album(key);}},[{kind:'album',key:'a'},{kind:'album',key:'b'}]),/offline/);
let active=true;
await assert.rejects(resolveCollections({...api,playlist:async()=>{active=false;return {songs:[song(1)]};}},[{kind:'playlist',key:'p'}],()=>active),/cancelled/);
assert.deepEqual(await resolveCollections(api,[]),[]);
console.log('Collection ordering, credits, occurrences and atomic resolution passed.');
