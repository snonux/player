import assert from 'node:assert/strict';
globalThis.window={addEventListener(){}};
const { mountMusicMix } = await import('../music-mix.js');
const { queueState } = await import('../queue.js');
import { API } from '../api.js';
import { state } from '../state.js';
import { beginPlaybackIntent } from '../playback-intent.js';
const nodes=[];
globalThis.document={createElement(){
 const button={disabled:false},select={value:'4',disabled:false},status={textContent:''};
 const section={isConnected:true,setAttribute(){},querySelector(s){return s==='button'?button:s==='select'?select:status;},button,select,status};
 nodes.push(section); return section;
}};
state.user={id:1};
const requests=[];
API.musicMix=(kind,key,rating)=>new Promise((resolve,reject)=>requests.push({kind,key,rating,resolve,reject}));
const host={append(){}};
const first=mountMusicMix(host,'song','1',()=>true);
const second=mountMusicMix(host,'artist','artist',()=>true);
const a=first.button.onclick(),b=second.button.onclick();
assert.equal(requests[0].rating,4);
requests[0].resolve({title:'Old seed',songs:[{id:1,type:'audio'}]});await a;
assert.equal(queueState().entries.length,0,'older pending mix must not replace queue');
requests[1].resolve({title:'Artist seed',songs:[]});await b;
assert.match(second.status.textContent,/No matching songs for Artist seed/);
assert.equal(queueState().entries.length,0,'no match preserves queue');
const failed=first.button.onclick();requests[2].reject(new Error('offline'));await failed;
assert.match(first.status.textContent,/Could not start mix.*offline/);
assert.equal(queueState().entries.length,0);
const changed=first.button.onclick();state.user={id:2};
requests[3].resolve({title:'Other account',songs:[{id:2,type:'audio'}]});await changed;
assert.equal(queueState().entries.length,0,'account switch cancels pending mix');
const replaced=first.button.onclick();beginPlaybackIntent();
requests[4].resolve({title:'Earlier click',songs:[{id:3,type:'audio'}]});await replaced;
assert.equal(queueState().entries.length,0,'new direct playback cancels pending mix');
assert.equal(first.button.disabled,false);
console.log('Mix actual action listeners: rating, newer intent, account, no-match and failure boundaries passed.');

// The listener-to-queue boundary verifies exact ranked order and replacement
// options, plus accepted/rejected/thrown and busy outcomes. Native tests below
// this boundary additionally exercise the actual queue actor and audio loader.
for (const outcome of ['accepted','rejected','thrown','busy']) {
 let calls=0;
 const section=mountMusicMix(host,'album','album',()=>true,{
  queue:()=>({loading:outcome==='busy'}),
  insert:async(songs,action,options)=>{
   calls++; assert.deepEqual(songs.map(song=>song.id),[4,2]);
   assert.equal(action,'now'); assert.deepEqual(options,{replace:true,source:'Mix · Album seed'});
   beginPlaybackIntent();
   if(outcome==='thrown')throw new Error('source failed');
   return outcome==='accepted';
  }
 });
 const before=requests.length, operation=section.button.onclick();
 if(outcome!=='busy')requests[before].resolve({title:'Album seed',songs:[{id:4,type:'audio'},{id:2,type:'audio'}]});
 await operation;
 assert.equal(calls,outcome==='busy'?0:1);
 assert.match(section.status.textContent,outcome==='accepted'?/Mix · Album seed · 2 songs/:outcome==='rejected'?/Playback was not started/:outcome==='thrown'?/Could not start mix.*source failed/:/Loading track/);
 if(outcome!=='accepted')assert.doesNotMatch(section.status.textContent,/Album seed · 2 songs/);
}
console.log('Mix insertion boundary: exact order/replacement, accepted/rejected/error/busy outcomes passed.');
