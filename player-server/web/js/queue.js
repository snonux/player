import { beginPlaybackIntent, isPlaybackIntent } from './playback-intent.js';
import { API } from './api.js';
import { currentMediaId, selectAndPlay, stopAndClose } from './player.js';

// One in-memory album queue, independent of the currently browsed library.
let entries = [], index = -1, label = '', activeID = null, serial = 0;
let revision = 0, loading = false, undo = null, status = '';
const notify = () => document.dispatchEvent(new Event('queue:changed'));
export function queueState() {
  return { entries: [...entries], index, label, active: activeID !== null && String(currentMediaId()) === String(activeID), loading, canUndo: !!undo, status };
}
export function deactivateQueue() {
  beginPlaybackIntent();
  revision++; activeID = null; loading = false; undo = null;
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
  index = at; label = source || songs[at]?.music?.album || 'Album'; undo = null;
  await jumpQueue(index);
}
export async function jumpQueue(at) {
  if (!entries[at]) return;
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
  if (target >= 0 && target < entries.length) await jumpQueue(target);
  return true;
}
function remember() { undo = { entries: [...entries], index }; }
export function moveQueue(at, delta) {
  const target = at + delta;
  if (loading || !entries[at] || target < 0 || target >= entries.length) return;
  remember();
  const current = entries[index];
  const [entry] = entries.splice(at, 1); entries.splice(target, 0, entry);
  index = entries.indexOf(current); status = 'Track moved. Undo available.'; notify();
}
export function removeQueue(at) {
  if (loading || !entries[at]) return;
  const previous = { entries: [...entries], index }, current = entries[index];
  if (at === index && queueState().active) stopAndClose();
  entries.splice(at, 1);
  index = at === index ? Math.min(at, entries.length - 1) : entries.indexOf(current);
  undo = previous; status = 'Track removed. Queue paused if it was playing. Undo available.'; notify();
}
export function clearQueue() {
  if (loading || !entries.length) return;
  const previous = { entries: [...entries], index };
  if (queueState().active) stopAndClose();
  revision++; activeID = null; entries = []; index = -1;
  undo = previous; status = 'Queue cleared. Undo available.'; notify();
}
export function undoQueue() {
  if (loading || !undo) return;
  const current = entries[index];
  entries = undo.entries;
  index = activeID !== null && entries.includes(current) ? entries.indexOf(current) : undo.index;
  undo = null; status = 'Queue restored. Choose a track to play.'; notify();
}
