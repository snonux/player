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
const button = () => ({disabled:false,isConnected:true,focus(){document.activeElement=this;},getClientRects:()=>[{}]});
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
