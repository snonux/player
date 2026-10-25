import assert from 'node:assert/strict';
const values=new Map(),stores=new Map(),events=[];
globalThis.localStorage={getItem:k=>values.get(k)||null,setItem:(k,v)=>values.set(k,v),removeItem:k=>values.delete(k)};
globalThis.location={origin:'http://player.test',pathname:'/index.html',search:''};
globalThis.document={dispatchEvent:e=>events.push(e.type)};
Object.defineProperty(globalThis,'navigator',{value:{onLine:true},configurable:true});
globalThis.caches={open:async name=>{if(!stores.has(name))stores.set(name,new Map());const rows=stores.get(name);return {match:async url=>rows.get(url)?.clone(),put:async(url,res)=>rows.set(url,new Response(await res.arrayBuffer(),{headers:res.headers})),delete:async url=>rows.delete(url)};}};
const {state}=await import('../state.js');
const {GlobalSyncCoordinator}=await import('../global-sync.js');
const {API}=await import('../api.js');
const {automaticPolicy,resolveAutomaticMusic,syncAutomaticMusic,saveAutomaticMusicPolicy,automaticMusicStatus,automaticMusicDue,cancelAutomaticMusic}=await import('../automatic-offline.js');
const {pinMusicCollection,offlinePinStatus,unpinMusicCollection,offlineSavedCollections,pinCollectionKey}=await import('../offline-pins.js');
const song=id=>({id,type:'audio',file_name:`${id}.mp3`,created_at:new Date(Date.UTC(2026,0,id)).toISOString()});
const bytes=()=>new Response(new Uint8Array([1,2,3]),{headers:{'content-type':'audio/mpeg','content-length':'3'}});
const playlist={kind:'playlist',key:'p'};
const deferred=()=>{let resolve;const promise=new Promise(r=>resolve=r);return {promise,resolve};};
assert.deepEqual(automaticPolicy(),{enabled:false,playlists:false,starred:false,recent:false,wifiOnly:true,hours:6});
state.user={id:1};values.set('player-music-network',JSON.stringify({mode:'wifi'}));
globalThis.fetch=async()=>bytes();
await pinMusicCollection(playlist,'Old',[song(1)]);
saveAutomaticMusicPolicy({enabled:true,playlists:true});
API.offlineMusicRead=async()=>{throw Error('offline detail');};
await assert.rejects(syncAutomaticMusic({force:true}),/offline detail/);
assert.deepEqual((await offlinePinStatus(playlist)).songs.map(s=>s.media.id),[1]);assert.equal(automaticMusicStatus().lastSuccess,undefined);
API.offlineMusicRead=async()=>({name:'New',songs:[song(2),song(3)]});
await syncAutomaticMusic({force:true});assert.deepEqual((await offlinePinStatus(playlist)).songs.map(s=>s.media.id),[2,3]);assert.equal(automaticMusicStatus().retained,2,'borrowed manual copy never becomes automatically owned');assert.equal(automaticMusicDue(),false);
API.offlineMusicRead=async()=>({name:'Empty',songs:[]});await syncAutomaticMusic({force:true});assert.deepEqual((await offlinePinStatus(playlist)).urls,[]);assert.equal(automaticMusicStatus().retired,2,'retired copies remain reachable');assert.ok((await offlineSavedCollections()).find(c=>c.collection.key==='@automatic-retained').songs.length===2);
// Cancellation between fetch and byte completion cannot publish membership.
const gate=deferred(),entered=deferred();globalThis.fetch=async()=>{entered.resolve();await gate.promise;return bytes();};API.offlineMusicRead=async()=>({name:'Late',songs:[song(4)]});const pending=syncAutomaticMusic({force:true});await entered.promise;cancelAutomaticMusic();gate.resolve();await assert.rejects(pending);assert.deepEqual((await offlinePinStatus(playlist)).urls,[]);assert.match(automaticMusicStatus().message,/Canceled/);
// Policy/account changes invalidate the old run and never overwrite new account status.
const readGate=deferred();API.offlineMusicRead=async()=>{await readGate.promise;return {songs:[song(5)]};};const old=syncAutomaticMusic({force:true});await Promise.resolve();state.user={id:2};saveAutomaticMusicPolicy({enabled:true,recent:true,wifiOnly:false});readGate.resolve();await assert.rejects(old);assert.equal(automaticMusicStatus().lastAttempt,undefined);assert.equal(automaticMusicStatus().policy.recent,true);
state.user={id:1};values.set('player-music-network',JSON.stringify({mode:'auto'}));let reads=0;API.offlineMusicRead=async()=>{reads++;return [];};await assert.rejects(syncAutomaticMusic({force:true}),/waits for Wi-Fi/);assert.equal(reads,0,'Firefox/unknown bearer never inferred Wi-Fi');assert.equal(automaticMusicStatus().error,'policy');
// A skipped interval does not turn incomplete downloads into global success.
const global=new GlobalSyncCoordinator({storage:localStorage,now:()=>1234,domains:{pending:()=>({ids:[],podcasts:0}),podcasts:async()=>{},progress:async()=>{},queue:async()=>{},music:explicit=>syncAutomaticMusic({force:explicit})}});global.bind('test');
await global.syncNow({explicit:false});assert.equal(global.state.lastFullSuccessMs,null);assert.equal(global.state.errorKind,'policy');assert.equal(reads,0);
// Production resolver paginates favorites and intersects authorized catalog music.
const calls=[];const result=await resolveAutomaticMusic({starred:true,recent:true},[],async path=>{calls.push(path);if(path==='/api/music/albums')return [{album_key:'a'}];if(path.includes('/albums/'))return {songs:Array.from({length:55},(_,i)=>song(i+1))};if(path.endsWith('offset=0'))return Array.from({length:200},(_,i)=>song(i+1));return [song(900)];});
assert.match(calls.at(-1),/offset=200/);assert.equal(result.snapshots[0].songs.length,55);assert.deepEqual(result.snapshots[1].songs.map(s=>s.id),Array.from({length:50},(_,i)=>55-i));
await assert.rejects(resolveAutomaticMusic({recent:true},[],async()=>({})),/Incomplete/);
await assert.rejects(resolveAutomaticMusic({recent:true},[],async()=>[],()=>false),/Canceled/);
// Unpin during an authoritative fetch is preserved; no late replacement.
values.set('player-music-network',JSON.stringify({mode:'wifi'}));globalThis.fetch=async()=>bytes();await pinMusicCollection(playlist,'Old',[song(1)]);
const unpinGate=deferred(),unpinEntered=deferred();API.offlineMusicRead=async()=>{unpinEntered.resolve();await unpinGate.promise;return {songs:[song(2)]};};const unpinRun=syncAutomaticMusic({force:true});await unpinEntered.promise;await unpinMusicCollection(playlist);unpinGate.resolve();await assert.rejects(unpinRun,/Pinned playlists changed/);assert.equal((await offlinePinStatus(playlist)).pinned,false);
// Retained-copy capacity excludes borrowed manual bytes; deletion frees capacity.
state.user={id:3};const capName='player-offline-v1-3',capRows={songs:{},collections:{}};const capCache=await caches.open(capName);
for(let id=1;id<=200;id++){const url=`http://player.test/api/media/${id}/download`;capRows.songs[url]={url,media:song(id),automatic:true,size:3,profile:'original'};await capCache.put(url,bytes());}
const retainedCollection={kind:'playlist',key:'@automatic-retained'};const retainedURLs=Object.keys(capRows.songs);capRows.collections[pinCollectionKey(retainedCollection)]={title:'Automatic music · retained downloads',urls:retainedURLs,ownedURLs:retainedURLs,pinned:false,automatic:true};
const borrowed='http://player.test/api/media/999/download';capRows.songs[borrowed]={url:borrowed,media:song(999),size:3,profile:'original'};await capCache.put(borrowed,bytes());values.set(`${capName}:index`,JSON.stringify(capRows));
saveAutomaticMusicPolicy({enabled:true,recent:true});let selected=999;API.offlineMusicRead=async path=>path==='/api/music/albums'?[{album_key:'a'}]:{songs:[song(selected)]};await syncAutomaticMusic({force:true});assert.equal(automaticMusicStatus().retained,200);
selected=1000;await assert.rejects(syncAutomaticMusic({force:true}),/200-copy limit/);await unpinMusicCollection(retainedCollection,{remove:true});await syncAutomaticMusic({force:true});assert.equal(automaticMusicStatus().retained,1,'actual retained cleanup frees the 200-copy capacity');assert.equal(JSON.parse(values.get(`${capName}:index`)).songs[borrowed].automatic,undefined);
// Automatic snapshots own current membership only. Real cleanup removes retired
// bytes after replacement/deselection and preserves active/manual overlaps.
state.user={id:5};saveAutomaticMusicPolicy({enabled:true,recent:true});let members=[1,2,3];API.offlineMusicRead=async path=>path==='/api/music/albums'?[{album_key:'a'}]:{songs:members.map(song)};await syncAutomaticMusic({force:true});
await pinMusicCollection({kind:'album',key:'manual'},'Manual overlap',[song(1)]);members=[2];await syncAutomaticMusic({force:true});
let index=()=>JSON.parse(values.get('player-offline-v1-5:index'));const recentKey=pinCollectionKey({kind:'playlist',key:'@automatic-recent'});assert.deepEqual(index().collections[recentKey].ownedURLs,['http://player.test/api/media/2/download']);
await unpinMusicCollection(retainedCollection,{remove:true});assert.deepEqual(Object.values(index().songs).map(row=>row.media.id).sort(),[1,2],'active and manual copies survive; retired3 is removed');
saveAutomaticMusicPolicy({enabled:true,starred:true,recent:false});API.offlineMusicRead=async path=>path==='/api/music/albums'?[{album_key:'a'}]:path.includes('/media?')?[]:{songs:[song(2)]};await syncAutomaticMusic({force:true});assert.deepEqual(index().collections[recentKey].ownedURLs,[],'deselected category retains no historical ownership');
await unpinMusicCollection(retainedCollection,{remove:true});assert.deepEqual(Object.values(index().songs).map(row=>row.media.id),[1],'deselection frees formerly active2 while manual overlap1 stays');
// A fresh module recovers the durable real start checkpoint after process death.
state.user={id:6};saveAutomaticMusicPolicy({enabled:true,recent:true});const deathGate=deferred(),deathEntered=deferred();API.offlineMusicRead=async()=>{deathEntered.resolve();await deathGate.promise;return [];};const abandoned=syncAutomaticMusic({force:true});await deathEntered.promise;
const durableKey='player-automatic-music-v1:http://player.test:6';assert.equal(JSON.parse(values.get(durableKey)).unfinishedAttempt,true);const restarted=await import('../automatic-offline.js?restart-after-death');assert.equal(restarted.automaticMusicStatus().error,'interrupted');assert.match(restarted.automaticMusicStatus().message,/interrupted/);
values.set('player-global-sync-v1:death',JSON.stringify({lastFullSuccessMs:17}));const restoredGlobal=new GlobalSyncCoordinator({storage:localStorage,now:()=>2000,domains:{pending:()=>({ids:[],podcasts:0}),podcasts:async()=>{},progress:async()=>{},queue:async()=>{},music:explicit=>restarted.syncAutomaticMusic({force:explicit})}});restoredGlobal.bind('death');await restoredGlobal.syncNow({explicit:false});assert.equal(restoredGlobal.state.lastFullSuccessMs,17);assert.equal(restoredGlobal.state.errorKind,'interrupted');
cancelAutomaticMusic();deathGate.resolve();await assert.rejects(abandoned);API.offlineMusicRead=async()=>[];await restoredGlobal.syncNow();assert.equal(restoredGlobal.state.lastFullSuccessMs,2000);assert.equal(restarted.automaticMusicStatus().unfinishedAttempt,false);assert.equal(restarted.automaticMusicStatus().error,null);
state.user={id:1};
// Broken browser storage fails honestly; no successful status is published.
values.set('player-automatic-music-v1:http://player.test:1','{broken');assert.throws(()=>automaticMusicStatus());
console.log('automatic offline: complete/empty membership, retained/manual copies, pagination, due, cancellation/account/policy/unpin and storage boundaries passed');
