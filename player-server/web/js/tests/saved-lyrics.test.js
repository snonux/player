import assert from 'node:assert/strict';
import {state} from '../state.js';
import {pinMusicCollection,offlinePinStatus,unpinMusicCollection} from '../offline-pins.js';
import {cacheSong,setPlaybackCacheAccount,playbackCacheSource} from '../playback-cache.js';
import {savedLyrics,snapshotLyrics} from '../saved-lyrics.js';
import {createServer} from 'node:http';
import {mkdtemp,writeFile,readFile,stat,utimes,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
const nativeFetch=globalThis.fetch;
const manifests=new Map(),stores=new Map();
globalThis.localStorage={getItem:k=>manifests.get(k)||null,setItem:(k,v)=>manifests.set(k,v),removeItem:k=>manifests.delete(k)};
globalThis.location={origin:'http://player.test',href:'http://player.test/',search:''};
globalThis.document={dispatchEvent:()=>{}};
Object.defineProperty(globalThis,'navigator',{value:{},configurable:true});
globalThis.caches={open:async name=>{if(!stores.has(name))stores.set(name,new Map());const rows=stores.get(name);return {match:async url=>rows.get(url)?.clone(),put:async(url,res)=>rows.set(url,new Response(await res.arrayBuffer(),{headers:res.headers})),delete:async url=>rows.delete(url)};},delete:async name=>stores.delete(name)};
const rich={source:'provider',language:'en',synced:true,lines:[{text:'Hello world',start:0,voice:'Lead',words:[{text:'Hello ',start:0},{text:'world',start:2}],translations:{fr:'Bonjour'}}]};
let calls=0;
globalThis.fetch=async(url,options={})=>{
 calls++;assert.notEqual(options.method,'POST','retention must never request provider egress');
 if(url.endsWith('/lyrics'))return Response.json(rich);
 return new Response(options.method==='HEAD'?null:new Uint8Array([1,2,3]),{headers:{'content-type':'audio/mpeg','content-length':'3',etag:'one'}});
};
state.user={id:7};setPlaybackCacheAccount(7);
const song={id:11,type:'audio',file_name:'song.mp3'},collection={kind:'album',key:'album'};
await pinMusicCollection(collection,'Album',[song]);
const row=(await offlinePinStatus(collection)).songs[0];
assert.equal(row.size,3,'integrity size remains audio-only');
assert.ok(row.lyricsSize>3);assert.ok(row.lyricsKey);
assert.ok(!manifests.get('player-offline-v1-7:index').includes('Hello world'),'body remains lazy');
const pinBacking={name:'player-offline-v1-7',url:row.url,etag:row.etag};
let saved=await savedLyrics({...song,offlinePinURL:row.url},row.url,undefined,pinBacking);
assert.deepEqual(saved.lines,rich.lines);assert.equal(saved.provider_enabled,false);
const count=calls;
await pinMusicCollection(collection,'Renamed album',[song]);
assert.equal(calls,count,'lightweight metadata refresh preserves existing lyric pointer/body');
assert.equal((await offlinePinStatus(collection)).songs[0].lyricsKey,row.lyricsKey);
globalThis.fetch=async()=>{throw new TypeError('offline');};
assert.deepEqual((await savedLyrics(song,row.url,undefined,pinBacking)).lines,rich.lines,'reads durable bodies without network');
state.user={id:8};assert.equal(await savedLyrics(song,row.url,undefined,pinBacking),null,'account scope');state.user={id:7};
await unpinMusicCollection(collection,{remove:true});
assert.equal(stores.get('player-offline-v1-7').size,0,'body removed with last owning audio');
globalThis.fetch=async(url,options={})=>url.endsWith('/lyrics')?Response.json(rich):new Response(options.method==='HEAD'?null:new Uint8Array([1,2,3]),{headers:{'content-type':'audio/mpeg','content-length':'3',etag:'one'}});
await cacheSong(song,'http://player.test/api/media/11/stream');
let index=JSON.parse(manifests.get('player-playback-v1-7:index'));
const first=Object.values(index)[0];assert.ok(first.lyricsSize);assert.ok(!JSON.stringify(index).includes('Hello world'));
assert.deepEqual((await savedLyrics(song,first.url,undefined,{name:'player-playback-v1-7',url:first.url,etag:first.etag})).lines,rich.lines);
// Seed accounting at the budget edge: audio fits but its optional body pushes
// the old copy over budget when another song arrives. No huge fixture needed.
first.size=128*1048576-3;first.used=0;manifests.set('player-playback-v1-7:index',JSON.stringify(index));
await cacheSong({...song,id:12},'http://player.test/api/media/12/stream');
index=JSON.parse(manifests.get('player-playback-v1-7:index'));
assert.equal(index[first.url],undefined,'lyric bytes participate in cache budget');
assert.equal(stores.get('player-playback-v1-7').has(first.lyricsKey),false,'eviction removes lazy body');

// Production selection identifies the exact backing store, URL/profile and
// served ETag. An older pin must never supply a newer cache copy's lyrics.
const olderURL='http://player.test/api/media/11/download',newerURL='http://player.test/api/media/11/compat?profile=aac128';
const oldKey=olderURL+'?player_lyrics=1',newKey=newerURL+'&player_lyrics=1';
const oldRow={media:song,url:olderURL,etag:'pin-a',size:3,used:0,lyricsKey:oldKey},newRow={media:song,url:newerURL,etag:'cache-b',size:3,used:1,lyricsKey:newKey};
manifests.set('player-offline-v1-7:index',JSON.stringify({songs:{[olderURL]:oldRow},collections:{}}));
manifests.set('player-playback-v1-7:index',JSON.stringify({[newerURL]:newRow}));
const pins=await caches.open('player-offline-v1-7'),playback=await caches.open('player-playback-v1-7');
const lyricBody=(text,etag)=>new Response(JSON.stringify({lines:[{text}]}),{headers:{'x-player-audio-etag':etag}});
await pins.put(olderURL,new Response('AAA',{headers:{etag:'pin-a'}}));await pins.put(oldKey,lyricBody('Older pin','pin-a'));
await playback.put(newerURL,new Response('BBB',{headers:{etag:'cache-b'}}));await playback.put(newKey,lyricBody('Newer cache','cache-b'));
globalThis.fetch=async()=>{throw new TypeError('offline');};
let backing;
assert.match(await playbackCacheSource(song,newerURL,undefined,value=>{backing=value;}),/^blob:/);
assert.equal((await savedLyrics(song,newerURL,undefined,backing)).lines[0].text,'Newer cache');
assert.equal(await savedLyrics(song,olderURL,undefined,backing),null,'different profile URL fails closed');
assert.equal(await savedLyrics(song,newerURL),null,'same media ID without backing proof fails closed');
const sameURLPin={...oldRow,url:newerURL};delete sameURLPin.lyricsKey;
manifests.set('player-offline-v1-7:index',JSON.stringify({songs:{[newerURL]:sameURLPin},collections:{}}));
await pins.put(newerURL,new Response('AAA',{headers:{etag:'pin-a'}}));
await playbackCacheSource(song,newerURL,undefined,value=>{backing=value;});
assert.equal(backing.name,'player-offline-v1-7');
assert.equal(await savedLyrics(song,newerURL,undefined,backing),null,'missing selected-pin lyrics cannot borrow another store copy');
sameURLPin.lyricsKey=newKey;manifests.set('player-offline-v1-7:index',JSON.stringify({songs:{[newerURL]:sameURLPin},collections:{}}));
await pins.put(newKey,lyricBody('New body replacing old snapshot','cache-b'));
assert.equal(await savedLyrics(song,newerURL,undefined,backing),null,'body overwritten during copy refresh cannot relabel old audio');
console.log('saved lyrics: exact selected store/profile/ETag, two-copy isolation and unknown-backing rejection passed');
console.log('saved lyrics: full model, lazy body, metadata preservation, no egress, account scope, cleanup and byte accounting passed');

assert.equal(await savedLyrics(null,''),null,'missing caller identity fails closed');
const directory=await mkdtemp(`${tmpdir()}/player-lyric-revision-`),source=`${directory}/source.audio`,modified=new Date('2020-01-01T00:00:00Z');
const fixture=createServer(async(request,response)=>{
 if(request.url.endsWith('/lyrics')) {
  await writeFile(source,'BBBB');await utimes(source,modified,modified);
  response.end(JSON.stringify({source:'sidecar',synced:true,lines:[{text:'B lyrics',start:0}]}));
 }else {
  const info=await stat(source,{bigint:true});
  response.setHeader('etag',`"${info.size}-${info.mtimeNs}-${info.ctimeNs}-${info.ino}"`);
  response.end(request.method==='HEAD'?undefined:await readFile(source));
 }
});
await new Promise(resolve=>fixture.listen(0,'127.0.0.1',resolve));
try {
 const origin=`http://127.0.0.1:${fixture.address().port}`;
 globalThis.fetch=(url,options)=>nativeFetch(new URL(url,origin),options);
 for(const route of ['download','stream','compat?profile=aac128','compat?profile=opus96']) {
  await writeFile(source,'AAAA');await utimes(source,modified,modified);
  const url=`${origin}/api/media/11/${route}`,response=await fetch(url),etag=response.headers.get('etag'),audio=await response.text();
  const bodies=await caches.open(`replacement-${route}`);
  assert.equal(await snapshotLyrics(11,url,etag,undefined,bodies),null,'B lyrics cannot attach to completed A audio');
  assert.equal(audio,'AAAA','metadata failure preserves audio');
  assert.equal((await stat(source)).mtime.getTime(),modified.getTime());
  assert.equal((await stat(source)).size,4);
 }
}finally {
 await new Promise(resolve=>fixture.close(resolve));await rm(directory,{recursive:true});
}
console.log('saved lyrics: original, encoded and cache snapshot rejection after same-size/preserved-mtime replacement passed');
