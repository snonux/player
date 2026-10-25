import assert from 'node:assert/strict';
import { webcrypto } from 'node:crypto';
const values = new Map(), events = new Map();
Object.defineProperty(globalThis,'crypto',{value:{getRandomValues: array => webcrypto.getRandomValues(array)},configurable:true}); // HTTP: no randomUUID.
globalThis.localStorage = {getItem: key => values.get(key) ?? null, setItem: (key,value) => values.set(key,value)};
globalThis.window = {addEventListener: (event,callback) => events.set(event,callback)};
globalThis.document = {dispatchEvent: () => {}};
globalThis.CustomEvent = class {constructor(type,options) {this.type=type;this.detail=options.detail;}};
const {API} = await import('../api.js');
const {listeningOccurrenceID,saveListeningProgress,setListeningProgressAccount,flushListeningProgress,listeningProgressStatus} = await import('../listening-progress.js');
const first = listeningOccurrenceID(), second = listeningOccurrenceID();
assert.match(first,/^[0-9a-f]{32}$/); assert.notEqual(first,second);
let online = false;
const sent = [];
API.progressBatch = async (rows,user) => {if (!online) throw new TypeError('offline');sent.push({rows,user});};
setListeningProgressAccount(3);
await saveListeningProgress(7,0,first);
await saveListeningProgress(7,10,first);
await saveListeningProgress(7,0,second);
assert.equal(JSON.parse(values.get('player-listening-progress-v1:3')).length,3);
await assert.rejects(flushListeningProgress(), /offline/);
assert.equal(listeningProgressStatus().pending.length, 3, 'global failure retains durable samples');
// Account transition never attributes the first account's queued plays to another.
setListeningProgressAccount(4);online=true;events.get('online')();
await new Promise(resolve => setImmediate(resolve));
assert.equal(sent.length,0);
setListeningProgressAccount(3);
await new Promise(resolve => setImmediate(resolve));
assert.equal(sent[0].user,3);
assert.deepEqual(sent[0].rows.map(r=>r.listening_session_id),[first,first,second]);
assert.equal(JSON.parse(values.get('player-listening-progress-v1:3')).length,0);
// Denied/quota storage retains online progress with the same stable occurrence.
localStorage.setItem = () => {throw new Error('storage denied');};
await saveListeningProgress(7,20,second);
assert.equal(sent.at(-1).rows[0].listening_session_id,second);
setListeningProgressAccount(null);

// A queued storage lock may outlive account switches or logout. Denied storage
// must not turn that old sample into an online save for the next credential.
for (const nextAccount of [4, null]) {
  setListeningProgressAccount(3);
  await new Promise(resolve => setImmediate(resolve));
  let release, entered;
  const held = new Promise(resolve => {release = resolve;});
  const acquired = new Promise(resolve => {entered = resolve;});
  Object.defineProperty(globalThis, 'navigator', {configurable: true, value: {locks: {
    request: async (_name, action) => {entered(); await held; return action();},
  }}});
  const before = sent.length;
  const saving = saveListeningProgress(7,30,first);
  await acquired;
  setListeningProgressAccount(nextAccount);
  release();
  await saving;
  assert.equal(sent.length,before,`stale denied-storage fallback after account ${nextAccount}`);
  setListeningProgressAccount(null);
  delete globalThis.navigator;
}
