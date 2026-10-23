import { API } from './api.js';

let account = null, flushing = false, retryTimer;
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
  account = Number.isSafeInteger(id) && id > 0 ? id : null;
  clearTimeout(retryTimer);
  document.dispatchEvent(new Event('listening-progress:account'));
  void flush();
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
  await flush();
}
async function flush() {
  const name = key();
  if (!name || flushing) return;
  flushing = true;
  try {
    while (key() === name) {
      const rows = read(name).slice(0, 100); if (!rows.length) break;
      await API.progressBatch(rows.map(({local_id, ...row}) => row), Number(name.slice(prefix.length)));
      if (key() !== name) break;
      const sent = new Set(rows.map(row => row.local_id));
      await exclusive(() => write(name, read(name).filter(row => !sent.has(row.local_id))));
    }
  } catch (error) {
    // Revoked/deleted sources must not block later authorized plays forever.
    if ([403,404].includes(error.status) && key() === name) {
      const index = /updates\[(\d+)\]/.exec(error.message)?.[1];
      if (index !== undefined) await exclusive(() => {const rows=read(name);rows.splice(Number(index),1);write(name,rows);});
    }
  } finally {
    flushing = false;
    clearTimeout(retryTimer);
    if (key() && read(key()).length) retryTimer = setTimeout(flush, 5000);
  }
}
window.addEventListener('online', () => void flush());
