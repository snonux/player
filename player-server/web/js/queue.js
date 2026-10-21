import { beginPlaybackIntent, isPlaybackIntent } from './playback-intent.js';
import { API } from './api.js';
import { currentMediaId, selectAndPlay, stopAndClose } from './player.js';

// One in-memory album queue, independent of the currently browsed library.
let entries = [], index = -1, label = '', activeID = null, serial = 0;
let revision = 0, loading = false, undo = null, status = '';
let nextIDs = new Set(), nextEpoch = 0;
function invalidateNext() { nextIDs.clear(); nextEpoch++; }
const notify = () => document.dispatchEvent(new Event('queue:changed'));
export function queueUndoToken() { return undo; }
export function queueState() {
  return { entries: entries.map(entry => ({ ...entry, upNext: nextIDs.has(entry.id) })), index, label, active: activeID !== null && String(currentMediaId()) === String(activeID), loading, canUndo: !!undo, status };
}
export function deactivateQueue() {
  beginPlaybackIntent();
  revision++; activeID = null; loading = false; undo = null; invalidateNext();
  if (entries.length) status = 'Queue paused. Choose a track to resume.';
  notify();
}
export function initQueue() {
  document.addEventListener('player:media-opened', e => {
    if (e.detail.restored || String(e.detail.media.id) !== String(activeID)) deactivateQueue();
  });
  document.addEventListener('player:closed', deactivateQueue);
}
export async function playAlbumCollection(songs, at = 0, source = '') {
  entries = songs.filter(song => song.type === 'audio').map(media => ({ id: ++serial, media }));
  invalidateNext();
  index = at; label = source || songs[at]?.music?.album || 'Album'; undo = null;
  await jumpQueue(index);
}
export async function jumpQueue(at, automatic = false) {
  if (!entries[at]) return;
  if (!automatic) invalidateNext();
  else nextIDs = new Set(entries.slice(at + 1).filter(entry => nextIDs.has(entry.id)).map(entry => entry.id));
  const operation = ++revision, intent = beginPlaybackIntent(), entry = entries[at];
  index = at; loading = true; status = 'Loading track…'; notify();
  let position = 0;
  if (entry.media.duration > 1200) {
    try { const p = (await API.mediaDetail(entry.media.id))?.progress; position = p?.finished ? 0 : p?.position_seconds || 0; } catch {}
  }
  if (operation !== revision || !isPlaybackIntent(intent) || entries[index]?.id !== entry.id) return;
  activeID = entry.media.id;
  selectAndPlay(entry.media, index, position);
  loading = false; status = ''; notify();
}
export async function advanceQueue(delta) {
  if (!loading && !queueState().active) return false;
  const target = index + delta;
  if (target >= 0 && target < entries.length) await jumpQueue(target, delta > 0);
  return true;
}
function remember() { undo = { entries: [...entries], index, nextIDs: new Set(nextIDs), nextEpoch, label }; }
export async function insertQueue(songs, action = 'next') {
  if (loading) return false;
  const added = songs.filter(song => song.type === 'audio').map(media => ({ id: ++serial, media }));
  if (!added.length) return false;
  remember();
  const previous = undo;
  let at = action === 'append' ? entries.length : Math.max(0, index + 1);
  if (action === 'next') {
    while (at < entries.length && nextIDs.has(entries[at].id)) at++;
  }
  entries.splice(at, 0, ...added);
  if (index < 0) index = 0;
  if (!label) label = 'Songs you added';
  if (action === 'next') added.forEach(entry => nextIDs.add(entry.id));
  if (action === 'now') {
    await jumpQueue(at);
    // A newer owner/action must never regain old Undo state.
    if (entries[index]?.id !== added[0].id || !queueState().active) return false;
    undo = previous;
  }
  status = `Added ${added.length} ${added.length === 1 ? 'song' : 'songs'}. Undo available.`;
  notify(); return true;
}
export function moveQueue(at, delta) {
  const target = at + delta;
  if (loading || !entries[at] || target < 0 || target >= entries.length) return;
  remember(); invalidateNext();
  const current = entries[index];
  const [entry] = entries.splice(at, 1); entries.splice(target, 0, entry);
  index = entries.indexOf(current); status = 'Track moved. Undo available.'; notify();
}
export function removeQueue(at) {
  if (loading || !entries[at]) return;
  remember();
  const previous = undo, current = entries[index];
  invalidateNext();
  if (at === index && queueState().active) stopAndClose();
  entries.splice(at, 1);
  index = at === index ? Math.min(at, entries.length - 1) : entries.indexOf(current);
  undo = previous; status = 'Track removed. Queue paused if it was playing. Undo available.'; notify();
}
export function clearQueue() {
  if (loading || !entries.length) return;
  remember();
  const previous = undo; invalidateNext();
  if (queueState().active) stopAndClose();
  revision++; activeID = null; entries = []; index = -1;
  undo = previous; status = 'Queue cleared. Undo available.'; notify();
}
export function undoQueue(expected) {
  if (expected && expected !== undo) return;
  if (loading || !undo) return;
  const current = entries[index], previous = undo;
  if (queueState().active && !previous.entries.includes(current)) stopAndClose();
  entries = previous.entries; label = previous.label;
  nextIDs = previous.nextEpoch === nextEpoch ? previous.nextIDs : new Set();
  index = activeID !== null && entries.includes(current) ? entries.indexOf(current) : previous.index;
  if (queueState().active) nextIDs = new Set(entries.slice(index + 1).filter(entry => nextIDs.has(entry.id)).map(entry => entry.id));
  undo = null; status = 'Queue restored. Choose a track to play.'; notify();
}
