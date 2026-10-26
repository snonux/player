import assert from 'node:assert/strict';
import { uniqueDownloadSongs } from '../music-downloads.js';
assert.deepEqual(uniqueDownloadSongs([{id:2,type:'audio'},{id:2,type:'audio'},{id:3,type:'video'},{id:1,type:'audio'},{id:-1,type:'audio'}]).map(s=>s.id),[2,1]);
console.log('Music download ID validation and deduplication passed.');

// Exercise the production document capture trap followed by dialog bubbling,
// as browsers dispatch Tab. A second local trap would move focus twice.
const { initModalFocus } = await import('../modal-focus.js');
const { downloadMusic } = await import('../music-downloads.js');
const listeners = new Map();
let overlay = null;
// Native disabling of the focused button releases focus to body.
const button = () => ({_disabled:false,isConnected:true,
 get disabled(){return this._disabled;},
 set disabled(value){this._disabled=value;if(value && document.activeElement===this)document.activeElement=document.body;},
 focus(){document.activeElement=this;},getClientRects:()=>[{}]});
const opener=button(),close=button(),start=button(),status={},progress={};
const panel = {
 className:'',innerHTML:'',
 querySelector:selector=>({'[data-status]':status,'[data-start]':start,'[data-close]':close,progress})[selector],
 querySelectorAll:()=>[close,start].filter(b=>!b.disabled),
 contains:element=>element===close || element===start,
 remove(){overlay=null;},
};
globalThis.document={
 activeElement:opener,
 addEventListener(type,handler,capture){if(capture)listeners.set(type,handler);},
 querySelectorAll(selector){return selector==='.modal-overlay.open' && overlay?[overlay]:[];},
 createElement:()=>panel,
 body:{append(element){overlay=element;}},
};
initModalFocus();
downloadMusic([{id:1,type:'audio'}],opener);
const key=(key,shiftKey=false)=>{
 const event={key,shiftKey,preventDefault(){},stopPropagation(){}};
 listeners.get('keydown')(event); // Capture phase.
 panel.onkeydown(event); // Bubble phase still runs after preventDefault.
};
assert.equal(document.activeElement,close);
key('Tab');assert.equal(document.activeElement,start,'Tab reaches Download ZIP');
key('Tab');assert.equal(document.activeElement,close,'Tab wraps to Close');
key('Tab',true);assert.equal(document.activeElement,start,'Shift+Tab reaches Download ZIP');
key('Escape');assert.equal(overlay,null);assert.equal(document.activeElement,opener,'Escape restores opener');
console.log('Music download dialog shares the capture Tab trap and restores focus.');

// Exercise the production mounted Start handler while the HTTP transfer is
// genuinely pending. Disabling Start must not strand keyboard focus on body:
// Escape must reach the modal's own cancellation/restoration path.
let requestAborted=false;
globalThis.fetch=(_url,options)=>new Promise((_resolve,reject)=>{
 options.signal.addEventListener('abort',()=>{requestAborted=true;reject(Error('aborted'));},{once:true});
});
downloadMusic([{id:1,type:'audio'}],opener);
start.focus();
const pending=start.onclick();
assert.equal(start.disabled,true);
assert.equal(document.activeElement,close,'starting moves focus from disabled Start to enabled Cancel');
assert.equal(close.textContent,'Cancel');
key('Escape');
await pending;
assert.equal(requestAborted,true,'Escape aborts the actual pending request');
assert.equal(overlay,null);
assert.equal(document.activeElement,opener,'Escape during transfer restores the initiating Download control');

// A programmatic start must leave another useful enabled focus target alone.
downloadMusic([{id:1,type:'audio'}],opener);
close.focus();
const otherPending=start.onclick();
assert.equal(document.activeElement,close);
key('Escape');await otherPending;
console.log('Starting a download preserves modal keyboard focus and Escape cancels the pending request.');
