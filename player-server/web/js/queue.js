import { offlineMusicMode } from './offline-context.js';
import { cachedMediaDetail } from './playback-cache.js';
import { preparedQueuePlan } from './queue-prepared.js';
import { SavedQueueSync, savedQueueRequest } from './queue-sync.js';
import { beginPlaybackIntent, currentPlaybackIntent, isPlaybackIntent } from './playback-intent.js';
import { queueStorageKey, readQueueSnapshot } from './queue-storage.js';
import { orderRemainder, shuffledCopy } from './queue-shuffle.js';
import { QueueAutoplay } from './queue-autoplay.js';
import { API } from './api.js';
import { currentMediaId, queuePlaybackPosition, selectAndPlay, stopAndClose } from './player.js';

// One in-memory album queue, independent of the currently browsed library.
let entries = [], index = -1, label = '', activeID = null, serial = 0;
let revision = 0, loading = false, undo = null, status = '';
let nextIDs = new Set(), nextEpoch = 0;
let albumLoading = false;
let repeat = 'off', shuffle = false, shuffleMode = 'tracks', canonical = [];
const autoplay = new QueueAutoplay();
export function setQueueAutoplay(enabled) {
  autoplay.setEnabled(enabled);
  notify();
}
function canonicalInsert(added, at) {
  const anchor = entries[at - 1]?.id;
  canonical.splice(anchor ? canonical.indexOf(anchor) + 1 : 0, 0, ...added.map(e => e.id));
}
export async function shuffleQueue({ reshuffle = false, mode = null } = {}) {
  if (loading || albumLoading || restoreError || ((!shuffle || reshuffle || mode) && eligibleShuffleCount() < 2)) return;
  const chosen = mode || shuffleMode;
  if (!['tracks', 'albums'].includes(chosen)) return;
  let albums = {};
  const enabled = reshuffle || mode !== null || !shuffle;
  if (enabled && chosen === 'albums') {
    const edits = queueChanges, scope = storageKey;
    albumLoading = true; status = 'Loading album order…'; document.dispatchEvent(new Event('queue:changed'));
    try { albums = offlineMusicMode() ? Object.fromEntries(entries.map((entry,order) => [entry.media.id, {album_key:JSON.stringify([entry.media.music?.album || entry.media.id, entry.media.music?.album_artist || entry.media.music?.artist || '']),order}])) : await API.queueAlbums([...new Set(entries.map(e => e.media.id))]); }
    catch { if (scope === storageKey) { albumLoading = false; status = 'Could not shuffle albums. Check your connection and retry.'; document.dispatchEvent(new Event('queue:changed')); } return; }
    if (edits !== queueChanges || scope !== storageKey) { if (scope === storageKey) { albumLoading = false; document.dispatchEvent(new Event('queue:changed')); } return; }
    albumLoading = false;
  }
  autoplay.cancel();
  remember(); shuffle = enabled; shuffleMode = chosen;
  entries = orderRemainder(entries, index, canonical, nextIDs, shuffle ? shuffleMode : false, Math.random, albums);
  status = `${shuffle ? (shuffleMode === 'albums' ? 'Upcoming albums shuffled; album song order preserved.' : 'Upcoming songs shuffled.') : 'Remaining song order restored.'} Undo available.`;
  notify();
}
function eligibleShuffleCount() { return entries.slice(index + 1).filter(e => !nextIDs.has(e.id)).length; }
function invalidateNext() { nextIDs.clear(); nextEpoch++; }
let storageKey = null, savedPosition = 0, positionEntry = null, restoreError = false, restored = false;
let applyingServerQueue = false, queueChanges = 0;
const portableSync = new SavedQueueSync({ storage: { getItem: key => localStorage.getItem(key), setItem: (key, value) => localStorage.setItem(key, value) }, request: savedQueueRequest, changed: () => document.dispatchEvent(new Event('queue:changed')) });
let lastSyncAt = 0;
const notify = () => { queueChanges++; saveQueue(); document.dispatchEvent(new Event('queue:changed')); };
function capturePosition() {
  if (!queueState().active || loading) return;
  const position = queuePlaybackPosition();
  if (Number.isFinite(position)) { savedPosition = position; positionEntry = entries[index]?.id; }
}
function portableSnapshot() {
 return { entries: entries.map(e => ({ id: e.id, media_id: e.media.id })), current_id: entries[index]?.id || 0, position_seconds: positionEntry === entries[index]?.id ? savedPosition : 0, label, repeat, shuffle, shuffle_mode: shuffleMode, canonical: [...canonical], up_next: [...nextIDs] };
}
export function savedSessionState() { return { conflict: !!portableSync.state.conflict, message: portableSync.message || '', available: !!portableSync.state.remote?.revision }; }
export function replaceSavedSession() { portableSync.capture(portableSnapshot()); return portableSync.replace(); }
export function keepOtherSession() { portableSync.keepOther(); }
export async function continueSavedSession() {
 const operation = revision, edits = queueChanges, intent = currentPlaybackIntent(), scope = storageKey;
 const remote = await portableSync.refresh();
 if (!remote || scope !== storageKey || operation !== revision || edits !== queueChanges || !isPlaybackIntent(intent)) return;
 await applySavedSession(remote, operation, edits, intent, scope);
}
async function applySavedSession(remote, operation, edits, intent, scope) {
 const media = new Map();
 try {
  for (const id of new Set(remote.entries.map(e => e.media_id))) {
   try { const detail = await cachedMediaDetail(id, () => API.mediaDetail(id)); if (detail?.media?.type === 'audio') media.set(id, detail.media); }
   catch(error) { if (![403,404,410].includes(error.status)) throw error; }
   if (operation !== revision || edits !== queueChanges || scope !== storageKey || !isPlaybackIntent(intent)) return;
  }
  applyingServerQueue = true;
  if (currentMediaId() !== null) stopAndClose();
  beginPlaybackIntent(); revision++;
  autoplay.reset();
  entries = remote.entries.filter(e => media.has(e.media_id)).map(e => ({id:e.id, media:media.get(e.media_id)}));
  index = entries.findIndex(e => e.id === remote.current_id);
  const selectedSurvived = index >= 0;
  if (!selectedSurvived) index = entries.length ? 0 : -1;
  savedPosition = selectedSurvived ? remote.position_seconds : 0; positionEntry = entries[index]?.id;
  if (entries[index]?.media.duration > 0) savedPosition = Math.min(savedPosition, entries[index].media.duration);
  serial = Math.max(serial, ...entries.map(e => e.id)); activeID = null; undo = null;
  label = remote.label; repeat = remote.repeat; shuffle = remote.shuffle; shuffleMode = remote.shuffle_mode || 'tracks';
  canonical = remote.canonical.filter(id => entries.some(e => e.id === id));
  nextIDs = new Set(remote.up_next.filter(id => entries.some(e => e.id === id)));
  restored = !!entries.length; restoreError = false; loading = false; status = 'Session continued · paused. Resume when you’re ready.';
  portableSync.adopt(remote, portableSnapshot()); notify();
 } catch { status = 'Can’t continue session. Check your connection and retry.'; document.dispatchEvent(new Event('queue:changed')); }
 finally { applyingServerQueue = false; }
}
function saveQueue() {
  if (!storageKey || restoreError) return;
  capturePosition();
  try {
    localStorage.setItem(storageKey, JSON.stringify({ version: 1,
      entries: entries.map(entry => ({ id: entry.id, mediaId: entry.media.id })), index,
      position: positionEntry === entries[index]?.id ? savedPosition : 0, label,
      repeat, shuffle, shuffle_mode: shuffleMode, canonical, upNext: [...nextIDs] }));
      if (!applyingServerQueue && !loading) {
      portableSync.capture(portableSnapshot());
      if (Date.now() - lastSyncAt > 15000) { lastSyncAt = Date.now(); void portableSync.sync(); }
    }
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
    autoplay.reset();
    entries = []; albumLoading = false; index = -1; activeID = null; label = ''; undo = null; repeat = 'off'; shuffle = false; shuffleMode = 'tracks'; canonical = [];
    restored = false; positionEntry = null; savedPosition = 0; invalidateNext();
  }
  storageKey = key;
  portableSync.bind(`${key}.server`);
  void portableSync.sync();
  const operation = ++revision;
  restoreError = true; loading = true; status = 'Restoring queue…'; notify();
  let snapshot;
  try { snapshot = readQueueSnapshot(localStorage.getItem(storageKey)); }
  catch {
    if (operation !== revision) return;
    try { localStorage.removeItem(storageKey); } catch {}
    repeat = 'off'; restoreError = false; loading = false; status = 'Saved queue was unreadable. Add songs to start again.'; notify(); return;
  }
  if (!snapshot) { portableSync.state.local = JSON.stringify(portableSnapshot()); restoreError = false; loading = false; status = ''; notify(); return; }
  try {
    // Deduplicate requests, never occurrences. A transient error keeps the original snapshot.
    const media = new Map();
    for (const id of new Set(snapshot.entries.map(entry => entry.mediaId))) {
      try { const detail = await cachedMediaDetail(id, () => API.mediaDetail(id)); media.set(id, detail?.media?.type === 'audio' ? detail.media : null); }
      catch (error) { if ([403, 404, 410].includes(error.status)) media.set(id, null); else throw error; }
      if (operation !== revision) return;
    }
    const current = snapshot.entries[snapshot.index];
    autoplay.reset();
    entries = snapshot.entries.filter(entry => media.get(entry.mediaId)).map(entry => ({ id: entry.id, media: media.get(entry.mediaId) }));
    index = entries.findIndex(entry => entry.id === current?.id);
    const keptCurrent = index >= 0;
    if (!keptCurrent) index = entries.length ? Math.min(snapshot.index, entries.length - 1) : -1;
    serial = Math.max(serial, ...snapshot.entries.map(entry => entry.id));
    repeat = snapshot.repeat ?? 'off';
    shuffle = snapshot.shuffle ?? false; shuffleMode = snapshot.shuffle_mode || 'tracks'; canonical = (snapshot.canonical ?? snapshot.entries.map(e => e.id)).filter(id => entries.some(e => e.id === id));
    label = snapshot.label; activeID = null; undo = null;
    nextIDs = new Set(snapshot.upNext.filter(id => entries.some(entry => entry.id === id)));
    positionEntry = entries[index]?.id; savedPosition = keptCurrent ? snapshot.position : 0;
    const duration = entries[index]?.media.duration;
    if (duration > 0) savedPosition = Math.min(savedPosition, duration);
    restored = entries.length > 0; loading = false; restoreError = false;
    portableSync.state.local = JSON.stringify(portableSnapshot());
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
  autoplay.cancel();
  repeat = { off: 'all', all: 'one', one: 'off' }[repeat];
  notify();
}

export function queueUndoToken() { return undo; }
export function queueState() {
  const autoplayStatus = autoplay.status;
  return {
    entries: entries.map(entry => ({ ...entry, upNext: nextIDs.has(entry.id) })),
    index, label,
    active: activeID !== null && String(currentMediaId()) === String(activeID),
    loading: loading || albumLoading || autoplay.loading,
    repeat, shuffle, shuffleMode, shuffleEligible: eligibleShuffleCount(),
    canUndo: !!undo,
    status: autoplayStatus || status,
    restored, restoreError,
    position: positionEntry === entries[index]?.id ? savedPosition : 0,
    autoplay: { enabled: autoplay.enabled, loading: autoplay.loading, status: autoplayStatus, available: !offlineMusicMode() },
  };
}
// Prepared playback is tied to queue occurrences, not media IDs (duplicates
// are legal), account scope, order and the active source generation.
export function preparedQueueNext() {
  if (!queueState().active || loading || restoreError || !entries[index]) return null;
  return preparedQueuePlan({ entries, index, scope: storageKey, revision, repeat });
}
export function commitPreparedQueueNext(token) {
  const next = preparedQueueNext();
  if (!next || next.token !== token) return false;
  const target = next.target;
  index = target; activeID = entries[index].media.id;
  savedPosition = 0; positionEntry = entries[index].id;
  nextIDs = new Set(entries.slice(index + 1).filter(entry => nextIDs.has(entry.id)).map(entry => entry.id));
  // The playback owner updates its identity before publishing queue changes,
  // so persistence cannot save the old transport's position under the new ID.
  return { media: entries[index].media, index, publish: notify };
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
  document.addEventListener('player:position', () => { capturePosition(); saveQueue(); });
  window.setInterval?.(() => { if (queueState().active) saveQueue(); }, 1000);
  window.addEventListener('online', () => void portableSync.sync());
  document.addEventListener('music-network:changed', () => void portableSync.sync());
  window.addEventListener('pagehide', () => { saveQueue(); void portableSync.sync(); });
  document.addEventListener('visibilitychange', () => { saveQueue(); void portableSync.sync(); });
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
  autoplay.reset();
  entries = songs.filter(song => song.type === 'audio').map(media => ({ id: ++serial, media }));
  invalidateNext(); shuffle = false; shuffleMode = 'tracks'; canonical = entries.map(e => e.id);
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
  if (!offlineMusicMode() && !restart && !(restored && positionEntry === entry.id) && entry.media.duration > 1200) {
    try { const p = (await API.mediaDetail(entry.media.id))?.progress; position = p?.finished ? 0 : p?.position_seconds || 0; } catch {}
  }
  if (operation !== revision || !isPlaybackIntent(intent) || entries[index]?.id !== entry.id) return;
  savedPosition = position; positionEntry = entry.id; restored = false; restoreError = false;
  activeID = entry.media.id;
  selectAndPlay(entry.media, index, position);
  loading = false; status = ''; notify();
}
async function tryAutoplayRelated() {
  const seed = entries[index];
  if (!seed || repeat !== 'off' || !autoplay.enabled) return false;
  const occurrence = seed.id;
  const pending = autoplay.resolve({
    seedId: seed.media.id,
    excluded: entries.map(entry => entry.media.id),
    online: !offlineMusicMode() && (typeof navigator === 'undefined' || navigator.onLine !== false),
    current: () => queueState().active && entries[index]?.id === occurrence,
    fetch: id => API.relatedMusic('song', String(id)),
  });
  notify();
  const songs = await pending;
  notify();
  if (!songs?.length || entries[index]?.id !== occurrence) return false;
  const at = entries.length;
  const added = songs.map(media => ({ id: ++serial, media, autoplayRelated: true }));
  canonical.push(...added.map(entry => entry.id));
  entries.push(...added);
  await jumpQueue(at, true, true);
  return true;
}
export async function advanceQueue(delta, { completed = false } = {}) {
  if (!loading && !queueState().active) return false;
  if ((loading && completed) || !entries.length) return true;
  let target = completed && repeat === 'one' ? index : index + delta;
  if (repeat === 'all') target = ((target % entries.length) + entries.length) % entries.length;
  if (target >= 0 && target < entries.length) {
    await jumpQueue(target, completed || delta > 0, false, completed && (repeat === 'one' || target === 0));
    return true;
  }
  if (completed && delta > 0 && repeat === 'off') await tryAutoplayRelated();
  return true;
}
function remember() { undo = { entries: [...entries], index, nextIDs: new Set(nextIDs), nextEpoch, label, shuffle, shuffleMode, canonical: [...canonical] }; }
export async function insertQueue(songs, action = 'next', {replace = false, source = 'Collection', shuffled = false} = {}) {
  if (loading) return false;
  const added = songs.filter(song => song.type === 'audio').map(media => ({ id: ++serial, media }));
  if (!added.length) return false;
  restoreError = false;
  autoplay.cancel();
  if (replace) autoplay.reset();
  remember();
  const previous = undo;
  if (replace) { entries = []; index = -1; label = source; invalidateNext(); shuffle = shuffled; shuffleMode = 'tracks'; canonical = []; if (shuffled) { const ordered = [...added]; added.splice(0, added.length, ...shuffledCopy(added)); canonical = ordered.map(e => e.id); } }
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
  autoplay.cancel();
  remember(); invalidateNext();
  const current = entries[index];
  const [entry] = entries.splice(at, 1); entries.splice(target, 0, entry);
  index = entries.indexOf(current); canonical = entries.map(e => e.id); status = 'Track moved. Undo available.'; notify();
}
export function removeQueue(at) {
  if (loading || !entries[at]) return;
  autoplay.cancel();
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
  autoplay.cancel();
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
  entries = previous.entries; label = previous.label; shuffle = previous.shuffle; shuffleMode = previous.shuffleMode; canonical = previous.canonical;
  nextIDs = previous.nextEpoch === nextEpoch ? previous.nextIDs : new Set();
  index = activeID !== null && entries.includes(current) ? entries.indexOf(current) : previous.index;
  if (queueState().active) nextIDs = new Set(entries.slice(index + 1).filter(entry => nextIDs.has(entry.id)).map(entry => entry.id));
  undo = null; status = 'Queue restored. Choose a track to play.'; notify();
}

// Switching local snapshots drops Undo and all old-account occurrences. It does
// not bind or overwrite authenticated account queue/session persistence.
export function resetOfflineQueue() {
 if (!offlineMusicMode()) return;
 beginPlaybackIntent(); revision++; activeID=null; entries=[]; index=-1;
 canonical=[]; undo=null; label=''; loading=false; albumLoading=false;
 restored=false; restoreError=false; savedPosition=0; positionEntry=null;
 repeat='off'; shuffle=false; shuffleMode='tracks'; invalidateNext();
 autoplay.reset();
 storageKey=null; status=''; document.dispatchEvent(new Event('queue:changed'));
}
