// Protocol adapter tests only: these do not represent browser/receiver acceptance.
import assert from 'node:assert/strict';
import { webcrypto } from 'node:crypto';
globalThis.crypto ??= webcrypto;
globalThis.window = { addEventListener() {} };
const timers = new Map(); let timerID = 0;
globalThis.setInterval = callback => { timers.set(++timerID, callback); return timerID; };
globalThis.clearInterval = id => timers.delete(id);
const requests = [], events = [];
let currentMedia = { id: 5, type: 'audio', duration: 20 };
let currentOccurrence = 'a'.repeat(32);
let heldMedia = null, releaseLoad = null, loadGate = null, failStop = false;
let state = { state: 'PLAYING', position: 8, duration: 20 }, unavailable = false;
globalThis.fetch = async (url, options = {}) => {
  const body = options.body ? JSON.parse(options.body) : null;
  requests.push({ url, body });
  if (failStop && body?.action === 'stop') return { ok: false };
  if (unavailable && body?.action === 'status') return { ok: false };
  let data = {};
  if (url === '/api/renderers') data = [{ id: 'opaque-id', name: 'Kitchen & dining' }];
  if (body?.action === 'status') data = state;
  if (body?.action === 'load') {
    if (body.media_id === heldMedia) await loadGate;
    data = { state: 'PLAYING', duration: 20 };
  }
  return { ok: true, status: 200, headers: { get: () => 'application/json' }, json: async () => data };
};
class Element {
  constructor(tag) { this.tag = tag; this.children = []; this.listeners = {}; this.dataset = {}; this.open = false; }
  append(...elements) { this.children.push(...elements); }
  insertBefore(element, before) { this.children.splice(this.children.indexOf(before), 0, element); }
  after(element) { this.afterElement = element; }
  setAttribute() {}
  addEventListener(type, callback) { this.listeners[type] = callback; }
  replaceChildren(...elements) { this.children = elements; }
  showModal() { this.open = true; }
  close() { this.open = false; this.listeners.close?.(); }
  remove() {}
  focus() { this.focused = true; }
  click() { return this.onclick?.() ?? this.listeners.click?.(); }
}
globalThis.document = { createElement: tag => new Element(tag), querySelector: () => null,
  body: new Element('body'), head: new Element('head'), dispatchEvent() {} };
const renderer = await import('../renderer.js');
const button = new Element('button');
renderer.initRenderer({ button, getCurrentMedia: () => currentMedia,
  localPosition: () => 4, getListeningOccurrence: () => currentOccurrence,
  pauseLocal: () => { events.push('local-paused'); events.push(['paused-id', currentMedia.id]); },
  onCastStarted: name => events.push(['started', name]), onRemoteTime: (...args) => events.push(['time', ...args]),
  onRemotePlaying: playing => events.push(['playing', playing]), onRemoteEnded: () => events.push('next'),
  onError: message => events.push(['error', message]), onCastEnded: (...args) => events.push(['return', ...args]),
}, document);
button.afterElement.click();
await new Promise(resolve => setTimeout(resolve, 0));
const dialog = document.body.children.at(-1);
const list = dialog.children.find(element => element.dataset.devices !== undefined);
await list.children[0].click();
assert.equal(renderer.rendererActive(), true);
assert(events.includes('local-paused'));
const initialLoad = requests.find(row => row.body?.action === 'load').body;
assert.deepEqual({ device_id: initialLoad.device_id, action: initialLoad.action, media_id: initialLoad.media_id, position: initialLoad.position },
  { device_id: 'opaque-id', action: 'load', media_id: 5, position: 4 });
assert.match(initialLoad.session_id, /^[a-f0-9]{32}$/);
assert.equal(initialLoad.revision, 1);
await [...timers.values()][0]();
assert.equal(renderer.rendererPosition(), 8);
await renderer.rendererSeek(12);
assert(requests.some(row => row.body?.action === 'seek' && row.body.position === 12));
state = { state: 'PLAYING', position: 19, duration: 20 };
await [...timers.values()][0]();
state = { state: 'STOPPED', position: 0, duration: 20 };
await [...timers.values()][0]();
assert(events.includes('next'), 'STOPPED with reset position after observed end advances queue');
currentMedia = { id: 6, type: 'audio' };
await renderer.rendererLoad(currentMedia);
unavailable = true;
for (let index = 0; index < 3; index++) await [...timers.values()][0]();
assert(events.some(row => Array.isArray(row) && row[0] === 'error' && row[1].includes('lost')));
assert.equal(renderer.rendererActive(), true, 'loss retains reconnect/return choice');
renderer.stopRenderer();
assert.equal(renderer.rendererActive(), false);
assert(events.some(row => Array.isArray(row) && row[0] === 'return' && row[1] === 6));
// Hold an initial LOAD while another track takes ownership. The pending
// transport routes the new selection remotely and old completion cannot pause it.
unavailable = false;
currentMedia = { id: 7, type: 'audio' };
heldMedia = 7; loadGate = new Promise(resolve => { releaseLoad = resolve; });
button.afterElement.click(); await new Promise(resolve => setTimeout(resolve, 0));
const picker = document.body.children.at(-1);
const devices = picker.children.find(element => element.dataset.devices !== undefined);
const selecting = devices.children[0].click();
await Promise.resolve();
assert.equal(renderer.rendererActive(), true, 'pending LOAD already owns the output');
currentMedia = { id: 8, type: 'audio' }; currentOccurrence = 'b'.repeat(32);
assert.equal(await renderer.rendererLoad(currentMedia), true);
releaseLoad(); await selecting;
assert.equal(renderer.rendererActive(), true, 'late initial selection cannot clear newer output');
assert.equal(events.filter(row => Array.isArray(row) && row[0] === 'paused-id' && row[1] === 8).length, 1, 'old completion does not pause newer source');
// STOP during a held LOAD carries a newer revision and clears ownership now.
currentMedia = { id: 9, type: 'audio' };
heldMedia = 9; loadGate = new Promise(resolve => { releaseLoad = resolve; });
const loading = renderer.rendererLoad(currentMedia);
const loadRequest = requests.at(-1).body;
await renderer.stopRenderer();
const stopRequest = requests.at(-1).body;
assert(stopRequest.revision > loadRequest.revision);
releaseLoad(); assert.equal(await loading, false);
assert.equal(renderer.rendererActive(), false, 'late LOAD cannot restore stopped ownership');
// A failed Stop must remain unsafe for callers selecting a local replacement,
// even after paused handback clears the active renderer UI.
heldMedia = null;
currentMedia = { id: 10, type: 'audio' };
button.afterElement.click(); await new Promise(resolve => setTimeout(resolve, 0));
const finalPicker = document.body.children.at(-1);
await finalPicker.children.find(element => element.dataset.devices !== undefined).children[0].click();
failStop = true;
assert.equal(await renderer.stopRenderer(), false);
assert.equal(renderer.rendererActive(), false, 'failed stop still hands back paused');
assert.equal(await renderer.stopRenderer(), false, 'retrying no active device must not erase uncertain remote stop');
const failedRevision = requests.at(-1).body.revision;
failStop = false;
assert.equal(await renderer.stopRenderer(), true, 'retained device and session allow acknowledged retry');
assert(requests.at(-1).body.revision > failedRevision);
assert.equal(await renderer.stopRenderer(), true, 'confirmed retry clears uncertainty');
assert(events.some(row => Array.isArray(row) && row[0] === 'error' && row[1].includes('may still be playing')));
console.log('Renderer protocol adapter tests passed.');
