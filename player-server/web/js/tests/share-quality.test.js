import assert from 'node:assert/strict';
import { bindShareQuality, shareQualityLabel } from '../share-quality.js';
function fixture() {
 const select={value:'',innerHTML:''},status={},reload={},handlers={};
 const panel={isConnected:true,querySelector:key=>key==='select'?select:key==='[role=status]'?status:reload,addEventListener:(name,fn)=>{handlers[name]=fn;}};
 return {root:{querySelector:()=>panel},panel,select,status,reload,handlers};
}
let resolveProfiles;
globalThis.fetch=()=>new Promise(resolve=>{resolveProfiles=resolve;});
const first=fixture();bindShareQuality(first.root,'original');
assert.equal(first.select.value,'original');
first.select.value=''; // user chooses Auto while profiles load
resolveProfiles({ok:true,json:async()=>[{id:'aac128',label:'Balanced · AAC 128 kbps'}]});
await new Promise(resolve=>setImmediate(resolve));
assert.equal(first.select.value,'','late profile response cannot undo Auto');
assert.match(first.select.innerHTML,/aac128/);
assert.match(first.status.textContent,/ready/);
let stopped=false;first.handlers.keydown({key:'Enter',target:{tagName:'SUMMARY'},stopPropagation(){stopped=true;}});assert.equal(stopped,true);
globalThis.fetch=async()=>{throw Error('offline');};
const offline=fixture();bindShareQuality(offline.root,'opus96');
await new Promise(resolve=>setImmediate(resolve));
assert.equal(offline.select.value,'opus96');
assert.match(offline.select.innerHTML,/Opus 96/);
assert.match(offline.status.textContent,/Reload/);
assert.match(shareQualityLabel('unknown'),/Unavailable/);
console.log('Share quality preserves explicit and Auto choices across loading/offline states.');
