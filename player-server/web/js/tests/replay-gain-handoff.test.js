import assert from 'node:assert/strict';
import { createLoudnessControls, replayGain } from '../replay-gain.js';
import { initDetach, toggleDetach, postToDetach, sendDetachedLoad } from '../detach.js';

function element(tagName = '') {
  const handlers = {};
  return { tagName, value: '', open: false, children: [], classList: { toggle() {}, add() {}, remove() {} },
    addEventListener(type, fn) { handlers[type] = fn; },
    emit(type, event = {}) { handlers[type]?.({ target: this, ...event }); },
    appendChild(value) { this.children.push(value); }, focus() { this.focused = true; },
  };
}
const panels = [], storageEvents = [];
globalThis.localStorage = { getItem() { throw Error('blocked'); }, setItem() { throw Error('blocked'); } };
globalThis.addEventListener = (type, handler) => { if (type === 'storage') storageEvents.push(handler); };
globalThis.document = { getElementById: () => null, createElement(tag) {
  const panel = element(tag.toUpperCase());
  const select = element('SELECT'), inputs = [element('INPUT'), element('INPUT')], summary = element('SUMMARY'), details = element('DETAILS'), status = element();
  details.querySelector = () => summary;
  panel.querySelector = selector => selector === 'select' ? select : selector === 'details' ? details : status;
  panel.querySelectorAll = () => inputs;
  panel.parts = { select, inputs, details, summary, status };
  if (tag === 'div') panels.push(panel);
  return panel;
} };
const controls = element();
let changed = 0;
const main = createLoudnessControls(controls, () => changed++);
main.set({mode:'automatic',preamp:2});
assert.equal(changed, 1);
const track = { id: 23, type:'audio', music:{replaygain_track_gain_db:-8,replaygain_album_gain_db:-4} };
let local = {media:track,currentTime:17,playing:true,loudness:{settings:main.settings,equalizer:{enabled:true,bands:[6,3,0,-2,-4]},albumContext:true}};
let receive, popupState = null;
const messages = [], restored = [];
const popup = { closed:false, postMessage: message => messages.push(message), close() { this.closed=true; }, __playerDetachCurrentState: () => popupState };
globalThis.window = { location:{origin:'http://local'}, open:() => popup, addEventListener:(_,fn) => receive=fn };
initDetach({ els:() => ({player:element()}), localPlaybackState:() => local, getCurrentMedia:() => track,
  currentMediaElement:() => ({pause(){}}), setPlaybackLoudness(value) { if (value) { main.set(value.settings, {announce:false});restored.push(value.settings.mode); } },
  loadMedia:() => restored.push('loaded'), requestPlay:() => restored.push('played') });
toggleDetach();
postToDetach({type:'detach-loudness-context',albumContext:false});
receive({origin:'http://local',source:popup,data:{type:'detach-ready'}});
assert.equal(messages.at(-1).type,'detach-load','pending context never replaces initial load');
assert.equal(messages.at(-1).loudness.albumContext,false,'queued context updates initial load');
local.loudness.albumContext=true;
sendDetachedLoad(track);
const payload = messages.at(-1);
assert.equal(payload.loudness.albumContext,true);
assert.deepEqual(payload.loudness.equalizer,{enabled:true,bands:[6,3,0,-2,-4]});
assert.equal(replayGain(track.music,payload.loudness.settings,payload.loudness.albumContext).db,-2,'ordered album applies album gain with preamp in popup');
assert.equal(replayGain(track.music,payload.loudness.settings,false).db,-6,'track result differs, so lost context is observable');
const detached = createLoudnessControls(element(), () => {});
detached.set(payload.loudness.settings, {announce:false});
const popupPanel = panels.at(-1);
popupPanel.parts.select.value='off'; popupPanel.emit('change');
popupState={...local,loudness:{settings:detached.settings,equalizer:{enabled:false,bands:[1,2,3,4,5]},albumContext:true}};
// Fresh synchronous popup state wins over the opener's stale async cache.
sendDetachedLoad({...track,id:24});
assert.equal(messages.at(-1).loudness.settings.mode,'off');
assert.deepEqual(messages.at(-1).loudness.equalizer,{enabled:false,bands:[1,2,3,4,5]},'fresh popup equalizer state survives track replacement with storage denied');
receive({origin:'http://local',source:{},data:{type:'detach-state',state:popupState}});
assert.equal(main.settings.mode,'automatic','unrelated source cannot update opener settings');
receive({origin:'http://local',source:popup,data:{type:'detach-state',state:popupState}});
assert.equal(main.settings.mode,'off','popup edit updates opener without localStorage');
main.set({mode:'track'}, {announce:false}); // Simulate stale opener memory at close.
restored.length=0;
toggleDetach();
assert.deepEqual(restored,['off','loaded','played'],'reattach restores owner settings before loading/playing');
assert.equal(main.settings.mode,'off');
storageEvents[0]({key:'player-loudness-v1',newValue:JSON.stringify({mode:'album'})});
assert.equal(main.settings.mode,'album','external preference refresh uses the production controls');
const advanced=panels[0].parts;
advanced.details.open=true;
let prevented=false;
advanced.details.emit('keydown',{key:'Escape',target:advanced.inputs[0],preventDefault(){prevented=true;},stopPropagation(){}});
assert(prevented);assert.equal(advanced.details.open,false);assert(advanced.summary.focused);
console.log('Loudness handoff preserves album context, pending loads, popup Off and in-memory preferences with storage denied.');

// Exercise compactControls' capture handler before the disclosure's bubbling
// handler: the first Escape must leave the surrounding popover open.
const { initCompactControls } = await import('../compactControls.js');
let captureKey, popoverOpen=false;
const create=document.createElement;
document.createElement=tag=>Object.assign(create(tag), {
  setAttribute(){},removeAttribute(){},append(...children){this.children.push(...children);},
  matches:()=>popoverOpen,hidePopover(){popoverOpen=false;},
});
document.addEventListener=(type,fn,capture)=>{if(type==='keydown'&&capture)captureKey=fn;};
globalThis.HTMLElement=class {showPopover(){}};
globalThis.matchMedia=()=>({matches:false,addEventListener(){}});
globalThis.MutationObserver=class {observe(){}};
const transport={insertBefore(){}}, playerControls=element(), player=element();
player.classList.contains=name=>name==='floating'||name==='open';
player.querySelector=selector=>selector==='.transport-controls'?transport:selector==='.controls'?playerControls:null;
player.querySelectorAll=()=>[];
initCompactControls(player);
popoverOpen=true;
const summary=element('SUMMARY'), details={open:true,querySelector:()=>summary};
let stopped=false;
captureKey({key:'Escape',target:{closest:()=>details},preventDefault(){},stopImmediatePropagation(){stopped=true;}});
assert(stopped);assert.equal(details.open,false);assert(summary.focused);assert(popoverOpen,'first Escape closes only Advanced loudness');
captureKey({key:'Escape',target:{closest:()=>null},preventDefault(){},stopImmediatePropagation(){}});
assert.equal(popoverOpen,false,'second Escape closes compact player options');
console.log('Compact player Escape closes Advanced loudness before its surrounding popover.');
