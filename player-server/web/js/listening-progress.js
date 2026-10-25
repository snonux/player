import { API } from './api.js';

let account = null, generation = 0, flushing = false, retryTimer, running, lastError;
const changed = () => document.dispatchEvent(new Event('listening-progress:changed'));
const exclusive = action => globalThis.navigator?.locks ? navigator.locks.request('player-listening-progress', action) : action();
const prefix = 'player-listening-progress-v1:';
const key = () => account ? `${prefix}${account}` : null;
function read(name) { try {return JSON.parse(localStorage.getItem(name) || '[]');} catch {return [];} }
function write(name, rows) {localStorage.setItem(name, JSON.stringify(rows));}
export function listeningOccurrenceID() {return Array.from(crypto.getRandomValues(new Uint8Array(16)), byte => byte.toString(16).padStart(2, '0')).join('');}

// Scope is set only after authenticated config resolves. Pending rows are never
// attributed to a different user, including after browser restart.
export function listeningProgressAccount() { return account; }
export function setListeningProgressAccount(id) {
  generation++;
  account = Number.isSafeInteger(id) && id > 0 ? id : null;
  clearTimeout(retryTimer);
  document.dispatchEvent(new Event('listening-progress:account'));
  void flush();
}
export function listeningProgressStatus() {
  return { account, generation, pending: key() ? read(key()).map(row => row.local_id) : [], lastError };
}
export async function flushListeningProgress() {
  const owner = generation, starting = new Set(listeningProgressStatus().pending);
  if (running) await running;
  if (owner !== generation) throw new Error('Account changed');
  await flush();
  if (owner !== generation) throw new Error('Account changed');
  if (listeningProgressStatus().pending.some(id => starting.has(id))) {
    throw lastError || new Error('Listening progress remains pending');
  }
}
export async function saveListeningProgress(mediaID, position, occurrence, observedAt = new Date().toISOString()) {
  const owner = account;
  const name = key();
  if (!name || !occurrence) return API.progress(mediaID, position);
  const row = {media_id: mediaID, position_seconds: position, listening_session_id: occurrence, observed_at: observedAt, local_id: listeningOccurrenceID()};
  try { await exclusive(() => {const rows = read(name); rows.push(row); write(name, rows);}); }
  catch {
    if (account !== owner) return;
    document.dispatchEvent(new CustomEvent('listening-progress:error', {detail: 'Browser storage is unavailable. Online listening saves continue; offline listening cannot be retained.'}));
    const {local_id, ...update} = row;
    return API.progressBatch([update], owner);
  }
  changed();
  await flush();
}
function flush() {
  if (running) return running;
  running = runFlush().finally(() => { running = null; });
  return running;
}
async function runFlush() {
  const name = key();
  if (!name || flushing) return;
  flushing = true;
  const owner = generation;
  const starting = new Set(read(name).map(row => row.local_id));
  lastError = null;
  try {
    while (key() === name && owner === generation) {
      const rows = read(name).filter(row => starting.has(row.local_id)).slice(0, 100); if (!rows.length) break;
      await API.progressBatch(rows.map(({local_id, ...row}) => row), Number(name.slice(prefix.length)));
      if (key() !== name || owner !== generation) break;
      const sent = new Set(rows.map(row => row.local_id));
      await exclusive(() => write(name, read(name).filter(row => !sent.has(row.local_id))));
    }
  } catch (error) {
    if (owner === generation) lastError = error;
    // Revoked/deleted sources must not block later authorized plays forever.
    if ([403,404].includes(error.status) && key() === name && owner === generation) {
      const index = /updates\[(\d+)\]/.exec(error.message)?.[1];
      if (index !== undefined) await exclusive(() => {const rows=read(name);rows.splice(Number(index),1);write(name,rows);});
    }
  } finally {
    flushing = false;
    clearTimeout(retryTimer);
    if (key() && read(key()).length) retryTimer = setTimeout(flush, 5000);
    changed();
  }
}
(globalThis.window ?? globalThis).addEventListener?.('online', () => void flush());
