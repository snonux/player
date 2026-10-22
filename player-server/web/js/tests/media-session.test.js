import assert from 'node:assert/strict';
import { createMediaSession } from '../media-session.js';
const handlers = {}, revoked = [], calls = [], pending = [];
let owns = true, serial = 0;
const el = { paused: false, ended: false, duration: 60, currentTime: 20, playbackRate: 1 };
const session = { setActionHandler: (name, fn) => { handlers[name] = fn; }, setPositionState: value => { session.position = value; } };
const os = createMediaSession({session, Metadata: class {constructor(data){Object.assign(this,data);}},
  getElement: () => el, ownsSource: () => owns,
  play: () => { calls.push('play'); el.paused = false; }, pause: () => {calls.push('pause');el.paused = true;},
  previous: () => calls.push('previous'), next: () => calls.push('next'),
  fetchImage: (url, options) => new Promise(resolve => pending.push({url,options,resolve})),
  urls: {createObjectURL: () => `blob:${++serial}`, revokeObjectURL: url => revoked.push(url)}});
const song = (id, artist, album) => ({id,type:'audio',file_name:`${id}.mp3`,music:{title:`Title ${id}`,artist,album}});
const settle = () => new Promise(resolve => setTimeout(resolve,0));
os.select(song(1,'Artist A','Album A'),'/protected/A');
assert.equal(session.metadata.title,'Title 1');assert.deepEqual(session.metadata.artwork,[]);
assert.equal(pending[0].options.credentials,'same-origin');
const staleNext = handlers.nexttrack;
os.select(song(2,'Artist B','Album B'),'/protected/B');
pending[0].resolve({ok:true,blob:async()=>({type:'image/png'})});await settle();
assert.deepEqual(session.metadata.artwork,[],'late previous source image rejected');
pending[1].resolve({ok:true,blob:async()=>({type:'image/png'})});await settle();
assert.equal(session.metadata.artist,'Artist B');assert.equal(session.metadata.album,'Album B');
assert.deepEqual(session.metadata.artwork,[{src:'blob:1'}]);
staleNext();assert.deepEqual(calls,[],'old source actions invalidated');
handlers.nexttrack();handlers.previoustrack();handlers.pause();os.refresh();assert.equal(session.playbackState,'paused');
handlers.play();handlers.seekbackward({seekOffset:5});assert.equal(el.currentTime,15);handlers.seekforward({});assert.equal(el.currentTime,30);handlers.seekto({seekTime:100});assert.equal(el.currentTime,60);
assert.deepEqual(calls,['next','previous','pause','play']);
os.select(song(3,'',''),'/missing');assert.deepEqual(revoked,['blob:1']);assert.deepEqual(session.metadata.artwork,[]);
pending[2].resolve({ok:false});await settle();assert.equal(session.metadata.title,'Title 3');assert.equal(session.metadata.artist,'Unknown artist');
os.select(song(4,'C','C'),'/late-account');owns=false;
pending[3].resolve({ok:true,blob:async()=>({type:'image/png'})});await settle();os.refresh();assert.equal(session.metadata,null);assert.equal(handlers.nexttrack,null);
owns=true;os.select({type:'image',file_name:'picture.jpg'},'image');assert.equal(session.metadata,null);
os.select({type:'audio',file_name:'untagged.mp3'});assert.equal(session.metadata.title,'untagged');os.clear();assert.equal(session.metadata,null);assert.equal(session.playbackState,'none');
console.log('Media Session metadata, ownership, artwork races and distinct transport/seek PASS');
