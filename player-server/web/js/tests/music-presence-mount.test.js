import assert from 'node:assert/strict';
// Plain HTTP on a LAN origin supplies getRandomValues, but no randomUUID.
const random = globalThis.crypto.getRandomValues.bind(globalThis.crypto);
Object.defineProperty(globalThis, 'crypto', {value:{getRandomValues:random},configurable:true});
assert.equal(crypto.randomUUID,undefined);
globalThis.window = new EventTarget();
globalThis.location = {origin:'http://player.lan'};
class Node extends EventTarget {
  constructor(tag) { super(); this.tagName=tag; this.children=[]; this.parts=new Map(); this.attrs=new Map(); }
  append(...nodes) { this.children.push(...nodes); }
  replaceChildren(...nodes) { this.children=nodes; }
  querySelector(selector) { if (!this.parts.has(selector)) this.parts.set(selector,new Node(selector)); return this.parts.get(selector); }
  setAttribute(name,value) { this.attrs.set(name,String(value)); }
  getAttribute(name) { return this.attrs.has(name)?this.attrs.get(name):null; }
  remove() { this.removed=true; }
  focus() {}
}
globalThis.document = Object.assign(new EventTarget(),{createElement:tag=>new Node(tag),getElementById:()=>null});
const calls=[];
globalThis.fetch=async (url,options)=>{calls.push({url,...options,body:options.body && JSON.parse(options.body)});return {ok:true,status:204};};
const {mountMusicPresence}=await import('../music-presence.js');
const parent=new Node('main');
const cleanup=mountMusicPresence(parent,{getAccount:()=>17,getSnapshot:()=>({media_id:3,state:'playing',position_seconds:5})});
assert.equal(parent.children.length,1,'actual mount completes without secure-context randomUUID');
const panel=parent.children[0], toggle=panel.querySelector('input');
assert.equal(panel.open,false,'Listeners stays collapsed by default');
assert.equal(panel.querySelector('summary')?.getAttribute('aria-expanded'),'false');
toggle.checked=true;toggle.dispatchEvent(new Event('change'));
await new Promise(resolve=>setImmediate(resolve));
const heartbeat=calls.find(call=>call.method==='PUT');
assert.ok(heartbeat,'mounted sharing control sends heartbeat');
assert.match(heartbeat.body.client_id,/^[0-9a-f]{32}$/,'uses existing secure random byte helper');
const hidden=new Event('pagehide');Object.defineProperty(hidden,'persisted',{value:true});window.dispatchEvent(hidden);
await new Promise(resolve=>setImmediate(resolve));
assert.equal(toggle.checked,false,'BFCache suspension disables sharing');
const beforeRestore=calls.filter(call=>call.method==='PUT').length;
const restored=new Event('pageshow');Object.defineProperty(restored,'persisted',{value:true});window.dispatchEvent(restored);
await new Promise(resolve=>setImmediate(resolve));
assert.equal(toggle.checked,false,'persisted restore never silently opts in');
assert.equal(calls.filter(call=>call.method==='PUT').length,beforeRestore);
toggle.checked=true;toggle.dispatchEvent(new Event('change'));
await new Promise(resolve=>setImmediate(resolve));
assert.equal(calls.filter(call=>call.method==='PUT').length,beforeRestore+1,'restored document rebinds account and explicit opt-in publishes again');
cleanup();
const afterCleanup=calls.length;window.dispatchEvent(restored);
await new Promise(resolve=>setImmediate(resolve));
assert.equal(calls.length,afterCleanup,'cleanup removes pageshow listener');
assert.equal(panel.removed,true);
console.log('music presence plain-HTTP mount PASS');
