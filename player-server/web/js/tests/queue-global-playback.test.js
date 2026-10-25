import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { webcrypto } from 'node:crypto';
import { currentPlaybackIntent } from '../playback-intent.js';
const values = new Map();
globalThis.localStorage = { getItem: key => values.get(key) || null, setItem: (key, value) => values.set(key, value) };
globalThis.location = { origin: 'https://example.com' };
globalThis.document = { dispatchEvent() {} };
Object.defineProperty(globalThis, 'crypto', { value: webcrypto, configurable: true });
globalThis.queueTestPlayer = { id: null, position: 0, playing: false, changes: 0 };
// Execute the actual queue module. Only the external playback port is replaced,
// so a mistaken applySavedSession/jump/stop in global sync is observable.
const player = `export const currentMediaId=()=>queueTestPlayer.id;
export const queuePlaybackPosition=()=>queueTestPlayer.position;
export const selectAndPlay=(media,index,position)=>{Object.assign(queueTestPlayer,{id:media.id,position,playing:true,changes:queueTestPlayer.changes+1});};
export const stopAndClose=()=>{queueTestPlayer.playing=false;queueTestPlayer.changes++;};`;
const playerURL = `data:text/javascript;base64,${Buffer.from(player).toString('base64')}`;
const queueURL = new URL('../queue.js', import.meta.url);
const source = (await readFile(queueURL, 'utf8')).replace(/from '(\.\/[^']+)'/g, (_all, relative) =>
  `from '${relative === './player.js' ? playerURL : new URL(relative, queueURL).href}'`);
const queue = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
let remote = { revision: 0, entries: [], current_id: 0, position_seconds: 0, label: '', repeat: 'off', shuffle: false, canonical: [], up_next: [] };
globalThis.fetch = async (_url, options) => {
  if (options.method === 'PUT') {
    if (options.headers['If-Match'] !== `"${remote.revision}"`) return new Response(JSON.stringify(remote), { status: 409, headers: { 'Content-Type': 'application/json' } });
    remote = { ...JSON.parse(options.body), revision: remote.revision + 1 };
  }
  return new Response(JSON.stringify(remote), { headers: { 'Content-Type': 'application/json' } });
};
await queue.restoreQueue(1); await queue.syncSavedSession();
await queue.insertQueue([{ id: 7, type: 'audio', duration: 100 }], 'now');
queueTestPlayer.position = 21;
await queue.syncSavedSession();
const before = { player: structuredClone(queueTestPlayer), queue: queue.queueState(), intent: currentPlaybackIntent() };
remote = { ...remote, revision: remote.revision + 10, entries: [{ id: 99, media_id: 77 }], current_id: 99, position_seconds: 88 };
await queue.syncSavedSession();
assert.deepEqual(queueTestPlayer, before.player, 'remote storage pull must not stop, seek or replace source');
assert.deepEqual(queue.queueState(), before.queue, 'remote storage pull leaves live queue unchanged');
assert.equal(currentPlaybackIntent(), before.intent, 'global retry does not claim playback intent');
await queue.insertQueue([{ id: 8, type: 'audio', duration: 100 }], 'append');
await assert.rejects(queue.syncSavedSession(), error => error.kind === 'conflict', 'remote refresh must not silently give the old live queue permission to overwrite a newer session');
assert.equal(remote.current_id, 99);
assert.deepEqual(queueTestPlayer, before.player);
console.log('Actual queue-module global pull preserves active source, position, playback and intent.');
