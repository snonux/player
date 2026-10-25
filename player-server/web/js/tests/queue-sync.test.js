import assert from 'node:assert/strict';
import { webcrypto } from 'node:crypto';
import { SavedQueueSync } from '../queue-sync.js';
if (!globalThis.crypto) globalThis.crypto = webcrypto;
const disk = new Map(), storage = {getItem: k => disk.get(k), setItem: (k,v) => disk.set(k,v)};
const q = {entries:[{id:1,media_id:7},{id:2,media_id:7}],current_id:2,position_seconds:7.25,label:'Repeated',repeat:'off',shuffle:false,canonical:[1,2],up_next:[]};
let server = {...q,revision:0}, writes=0;
const request = async (method, body, revision) => {
 if (method==='GET') return structuredClone(server);
 writes++;
 if (revision !== server.revision) return {conflict:true,queue:structuredClone(server)};
 server={...body,revision:revision+1}; return {conflict:false,queue:structuredClone(server)};
};
let sync = new SavedQueueSync({storage,request}); sync.bind('a');sync.capture(q);await sync.sync();
assert.equal(server.current_id,2);assert.equal(server.position_seconds,7.25);assert.equal(server.entries.length,2);
assert.equal(sync.state.pending,null);await sync.sync();assert.equal(sync.running,null);
// A stale offline snapshot cannot overwrite the newer session after restart.
sync.capture({...q,position_seconds:8});server={...server,revision:2,position_seconds:20};
sync=new SavedQueueSync({storage,request});sync.bind('a');await sync.sync();
assert.equal(sync.state.conflict,true);assert.equal(server.position_seconds,20);
sync.keepOther();sync.capture({...q,position_seconds:9});await sync.sync();assert.equal(server.position_seconds,20);
await sync.replace();assert.equal(server.position_seconds,9);assert.equal(server.revision,3);
// New device must make an explicit replacement choice, even with a local queue.
const fresh=new SavedQueueSync({storage,request});fresh.bind('b');fresh.capture(q);await fresh.sync();assert.equal(fresh.state.conflict,true);assert.equal(server.position_seconds,9);
// Late account response may not mutate the next account's sync state.
let resolve;const late=new SavedQueueSync({storage,request:()=>new Promise(r=>resolve=r)});late.bind('old');const running=late.sync();await Promise.resolve();late.bind('new');resolve({...q,revision:99});await running;assert.equal(late.state.revision,undefined);
// Lost acknowledgment replay is recognized by mutation identity after restart.
const replay=new SavedQueueSync({storage,request});replay.bind('replay');replay.capture(q);server={...replay.state.pending,revision:4};await replay.sync();assert.equal(replay.state.pending,null);assert.equal(replay.state.conflict,undefined);
assert.ok(writes>=3);
console.log('Saved queue conflict, restart, replay and late-account checks passed.');
const globalQueue = new SavedQueueSync({storage,request});
globalQueue.bind('global');
await globalQueue.syncForGlobal();
assert.equal(globalQueue.state.remote.revision,server.revision, 'global run retrieves remote storage');
globalQueue.capture({...q,position_seconds:91});
await assert.rejects(globalQueue.syncForGlobal(),error=>error.kind==='conflict');
assert.equal(globalQueue.state.pending.position_seconds,91);
const unavailable=new SavedQueueSync({storage,request:async()=>{throw Object.assign(new Error('auth'),{status:401});}});
unavailable.bind('unavailable');
await assert.rejects(unavailable.syncForGlobal(),error=>error.status===401,'quiet domain catch cannot report global success');
