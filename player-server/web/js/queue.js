import { beginPlaybackIntent, isPlaybackIntent } from './playback-intent.js';
import { queueStorageKey, readQueueSnapshot } from './queue-storage.js';
import { orderRemainder, shuffledCopy } from './queue-shuffle.js';
import { API } from './api.js';
import { currentMediaId, queuePlaybackPosition, selectAndPlay, stopAndClose } from './player.js';

// One in-memory album queue, independent of the currently browsed library.
let entries = [], index = -1, label = '', activeID = null, serial = 0;
let revision = 0, loading = false, undo = null, status = '';
let nextIDs = new Set(), nextEpoch = 0;
let repeat = 'off', shuffle = false, canonical = [];
function canonicalInsert(added, at) {
  const anchor = entries[at - 1]?.id;
  canonical.splice(anchor ? canonical.indexOf(anchor) + 1 : 0, 0, ...added.map(e => e.id));
}
export function shuffleQueue({ reshuffle = false } = {}) {
  if (loading || restoreError || (!shuffle && eligibleShuffleCount() < 2) || (reshuffle && eligibleShuffleCount() < 2)) return;
  remember();
  shuffle = reshuffle || !shuffle;
  entries = orderRemainder(entries, index, canonical, nextIDs, shuffle);
  status = `${shuffle ? 'Upcoming songs shuffled.' : 'Remaining song order restored.'} Undo available.`;
  notify();
}
function eligibleShuffleCount() { return entries.slice(index + 1).filter(e => !nextIDs.has(e.id)).length; }
function invalidateNext() { nextIDs.clear(); nextEpoch++; }
let storageKey = null, savedPosition = 0, positionEntry = null, restoreError = false, restored = false;
const notify = () => { saveQueue(); document.dispatchEvent(new Event('queue:changed')); };
function capturePosition() {
  if (!queueState().active || loading) return;
  const position = queuePlaybackPosition();
  if (Number.isFinite(position)) { savedPosition = position; positionEntry = entries[index]?.id; }
}
function saveQueue() {
  if (!storageKey || restoreError) return;
  capturePosition();
  try {
    localStorage.setItem(storageKey, JSON.stringify({ version: 1,
      entries: entries.map(entry => ({ id: entry.id, mediaId: entry.media.id })), index,
      position: positionEntry === entries[index]?.id ? savedPosition : 0, label,
      repeat, shuffle, canonical, upNext: [...nextIDs] }));
  } catch {
    const message = 'Queue could not be saved on this device.';
    if (status !== message) { status = message; document.dispatchEvent(new Event('queue:changed')); }
  }
}
export function recoveredQueuePosition(mediaId) {
  return restored && String(entries[index]?.media.id) === String(mediaId) && positionEntry === entries[index]?.id ? savedPosition : null;
}
export async function restoreQueue(userId) {
  if (!Number.isSafeInteger(userId) || userId <= 0) return;
  const key = queueStorageKey(location.origin, userId);
  if (storageKey !== key) {
    saveQueue();
    if (queueState().active) stopAndClose();
    beginPlaybackIntent();
    entries = []; index = -1; activeID = null; label = ''; undo = null; repeat = 'off'; shuffle = false; canonical = [];
    restored = false; positionEntry = null; savedPosition = 0; invalidateNext();
  }
  storageKey = key;
  const operation = ++revision;
  restoreError = true; loading = true; status = 'Restoring queue…'; notify();
  let snapshot;
  try { snapshot = readQueueSnapshot(localStorage.getItem(storageKey)); }
  catch {
    if (operation !== revision) return;
    try { localStorage.removeItem(storageKey); } catch {}
    repeat = 'off'; restoreError = false; loading = false; status = 'Saved queue was unreadable. Add songs to start again.'; notify(); return;
  }
  if (!snapshot) { restoreError = false; loading = false; status = ''; notify(); return; }
  try {
    // Deduplicate requests, never occurrences. A transient error keeps the original snapshot.
    const media = new Map();
    for (const id of new Set(snapshot.entries.map(entry => entry.mediaId))) {
      try { const detail = await API.mediaDetail(id); media.set(id, detail?.media?.type === 'audio' ? detail.media : null); }
      catch (error) { if ([403, 404, 410].includes(error.status)) media.set(id, null); else throw error; }
      if (operation !== revision) return;
    }
    const current = snapshot.entries[snapshot.index];
    entries = snapshot.entries.filter(entry => media.get(entry.mediaId)).map(entry => ({ id: entry.id, media: media.get(entry.mediaId) }));
    index = entries.findIndex(entry => entry.id === current?.id);
    const keptCurrent = index >= 0;
    if (!keptCurrent) index = entries.length ? Math.min(snapshot.index, entries.length - 1) : -1;
    serial = Math.max(serial, ...snapshot.entries.map(entry => entry.id));
    repeat = snapshot.repeat ?? 'off';
    shuffle = snapshot.shuffle ?? false; canonical = (snapshot.canonical ?? snapshot.entries.map(e => e.id)).filter(id => entries.some(e => e.id === id));
    label = snapshot.label; activeID = null; undo = null;
    nextIDs = new Set(snapshot.upNext.filter(id => entries.some(entry => entry.id === id)));
    positionEntry = entries[index]?.id; savedPosition = keptCurrent ? snapshot.position : 0;
    const duration = entries[index]?.media.duration;
    if (duration > 0) savedPosition = Math.min(savedPosition, duration);
    restored = entries.length > 0; loading = false; restoreError = false;
    const dropped = snapshot.entries.length - entries.length;
    status = entries.length ? 'Queue restored · paused. Resume when you’re ready.' : 'Queue is empty.';
    if (dropped) status += ` ${dropped} ${dropped === 1 ? 'song is' : 'songs are'} no longer available.`;
    notify();
  } catch {
    if (operation !== revision) return;
    loading = false; status = 'Can’t restore queue. Check your connection and retry.'; notify();
  }
}
export function retryQueueRestore() {
  const id = Number(storageKey?.slice(storageKey.lastIndexOf('.') + 1));
  return restoreQueue(id);
}
export function resumeQueue() { return jumpQueue(index, false, restored); }

export function cycleQueueRepeat() {
  if (loading || restoreError) return;
  repeat = { off: 'all', all: 'one', one: 'off' }[repeat];
  notify();
}

export function queueUndoToken() { return undo; }
export function queueState() {
  return { entries: entries.map(entry => ({ ...entry, upNext: nextIDs.has(entry.id) })), index, label, active: activeID !== null && String(currentMediaId()) === String(activeID), loading, repeat, shuffle, shuffleEligible: eligibleShuffleCount(), canUndo: !!undo, status, restored, restoreError, position: positionEntry === entries[index]?.id ? savedPosition : 0 };
}
export function deactivateQueue({ keepNext = false } = {}) {
  capturePosition();
  beginPlaybackIntent();
  revision++; activeID = null; loading = false; undo = null; if (!keepNext) invalidateNext();
  if (entries.length) status = 'Queue paused. Choose a track to resume.';
  notify();
}
export function initQueue() {
  const audio = document.getElementById('media-audio');
  for (const event of ['pause', 'seeked']) audio?.addEventListener(event, () => { capturePosition(); saveQueue(); });
  window.setInterval?.(() => { if (queueState().active) saveQueue(); }, 1000);
  window.addEventListener('pagehide', saveQueue);
  document.addEventListener('visibilitychange', saveQueue);
  document.addEventListener('player:media-opened', e => {
    if (e.detail.restored || String(e.detail.media.id) !== String(activeID)) {
      deactivateQueue({ keepNext: e.detail.restored && restored && String(e.detail.media.id) === String(entries[index]?.media.id) });
    }
  });
  document.addEventListener('player:closed', deactivateQueue);
  document.addEventListener('queue:repeat', cycleQueueRepeat);
}
export async function playAlbumCollection(songs, at = 0, source = '') {
  restoreError = false; restored = false; savedPosition = 0; positionEntry = null;
  entries = songs.filter(song => song.type === 'audio').map(media => ({ id: ++serial, media }));
  invalidateNext(); shuffle = false; canonical = entries.map(e => e.id);
  index = at; label = source || songs[at]?.music?.album || 'Album'; undo = null;
  await jumpQueue(index);
}
export async function jumpQueue(at, automatic = false, preserveNext = false, restart = false) {
  if (!entries[at]) return;
  if (!automatic && !preserveNext) invalidateNext();
  else nextIDs = new Set(entries.slice(at + 1).filter(entry => nextIDs.has(entry.id)).map(entry => entry.id));
  const operation = ++revision, intent = beginPlaybackIntent(), entry = entries[at];
  index = at; loading = true; status = 'Loading track…'; notify();
  let position = restored && positionEntry === entry.id ? savedPosition : 0;
  if (!restart && !(restored && positionEntry === entry.id) && entry.media.duration > 1200) {
    try { const p = (await API.mediaDetail(entry.media.id))?.progress; position = p?.finished ? 0 : p?.position_seconds || 0; } catch {}
  }
  if (operation !== revision || !isPlaybackIntent(intent) || entries[index]?.id !== entry.id) return;
  savedPosition = position; positionEntry = entry.id; restored = false; restoreError = false;
  activeID = entry.media.id;
  selectAndPlay(entry.media, index, position);
  loading = false; status = ''; notify();
}
export async function advanceQueue(delta, { completed = false } = {}) {
  if (!loading && !queueState().active) return false;
  if ((loading && completed) || !entries.length) return true;
  let target = completed && repeat === 'one' ? index : index + delta;
  if (repeat === 'all') target = ((target % entries.length) + entries.length) % entries.length;
  if (target >= 0 && target < entries.length) await jumpQueue(target, completed || delta > 0, false, completed && (repeat === 'one' || target === 0));
  return true;
}
function remember() { undo = { entries: [...entries], index, nextIDs: new Set(nextIDs), nextEpoch, label, shuffle, canonical: [...canonical] }; }
export async function insertQueue(songs, action = 'next', {replace = false, source = 'Collection', shuffled = false} = {}) {
  if (loading) return false;
  const added = songs.filter(song => song.type === 'audio').map(media => ({ id: ++serial, media }));
  if (!added.length) return false;
  restoreError = false;
  remember();
  const previous = undo;
  if (replace) { entries = []; index = -1; label = source; invalidateNext(); shuffle = shuffled; canonical = []; if (shuffled) { const ordered = [...added]; added.splice(0, added.length, ...shuffledCopy(added)); canonical = ordered.map(e => e.id); } }
  let at = action === 'append' ? entries.length : Math.max(0, index + 1);
  if (action === 'next') {
    while (at < entries.length && nextIDs.has(entries[at].id)) at++;
  }
  if (!replace || !shuffled) action === 'append' ? canonical.push(...added.map(e => e.id)) : canonicalInsert(added, at);
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
  index = entries.indexOf(current); canonical = entries.map(e => e.id); status = 'Track moved. Undo available.'; notify();
}
export function removeQueue(at) {
  if (loading || !entries[at]) return;
  remember();
  const previous = undo, current = entries[index];
  invalidateNext();
  if (at === index && queueState().active) stopAndClose();
  const removed = entries.splice(at, 1); canonical = canonical.filter(id => id !== removed[0].id);
  index = at === index ? Math.min(at, entries.length - 1) : entries.indexOf(current);
  undo = previous; status = 'Track removed. Queue paused if it was playing. Undo available.'; notify();
}
export function clearQueue() {
  if (loading || !entries.length) return;
  remember();
  const previous = undo; invalidateNext();
  if (queueState().active) stopAndClose();
  revision++; activeID = null; entries = []; canonical = []; index = -1; restored = false; restoreError = false;
  undo = previous; status = 'Queue cleared. Undo available.'; notify();
}
export function undoQueue(expected) {
  if (expected && expected !== undo) return;
  if (loading || !undo) return;
  const current = entries[index], previous = undo;
  if (queueState().active && !previous.entries.includes(current)) stopAndClose();
  entries = previous.entries; label = previous.label; shuffle = previous.shuffle; canonical = previous.canonical;
  nextIDs = previous.nextEpoch === nextEpoch ? previous.nextIDs : new Set();
  index = activeID !== null && entries.includes(current) ? entries.indexOf(current) : previous.index;
  if (queueState().active) nextIDs = new Set(entries.slice(index + 1).filter(entry => nextIDs.has(entry.id)).map(entry => entry.id));
  undo = null; status = 'Queue restored. Choose a track to play.'; notify();
}
