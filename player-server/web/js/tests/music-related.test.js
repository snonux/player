import assert from 'node:assert/strict';
import { API } from '../api.js';
import { mountRelatedMusic } from '../music-related.js';
import { queueState } from '../queue.js';
import { currentPlaybackIntent } from '../playback-intent.js';
import { state } from '../state.js';

// Exercise the actual installed Play listeners with independently deferred album
// reads. A must never mutate the queue after a newer click, even while B loads.
const buttons=[0,1].map(index=>({dataset:{relatedPlay:String(index)},disabled:false}));
const statuses=[{textContent:''},{textContent:''}];
const section={isConnected:true,innerHTML:'',setAttribute(){},
 querySelectorAll(selector){return selector==='[data-related-play]'?buttons:[];},
 querySelector(selector){return statuses[Number(selector.match(/"(\d+)"/)[1])];}
};
globalThis.document={createElement:()=>section,dispatchEvent(){},getElementById:()=>null};
state.user={id:1};
API.relatedMusic=async()=>['a','b'].map(key=>({kind:'album',key,title:key,basis:['Genre: Jazz']}));
let resolveA,resolveB;
API.album=key=>new Promise(resolve=>{if(key==='a')resolveA=resolve;else resolveB=resolve;});
mountRelatedMusic({append(){}},'album','source',()=>true,()=>{});
await new Promise(resolve=>setImmediate(resolve));
assert.equal(typeof buttons[0].onclick,'function');
const a=buttons[0].onclick(),intentA=currentPlaybackIntent();
const b=buttons[1].onclick(),intentB=currentPlaybackIntent();
assert.ok(intentB>intentA,'each click must claim playback ownership before resolving');
resolveA({songs:[{id:1,type:'audio',duration:10}]});
await a;
assert.equal(queueState().entries.length,0,'A cannot replace the queue while the newer B is still resolving');
assert.equal(buttons[1].disabled,true);
resolveB({songs:[]});
await b;
assert.equal(queueState().entries.length,0);
assert.equal(buttons[1].disabled,false);
assert.match(statuses[1].textContent,/not started/);
console.log('Related music actual Play listeners: newer click cancels deferred earlier resolution before queue mutation.');
