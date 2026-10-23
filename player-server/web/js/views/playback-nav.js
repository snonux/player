import { beginPlaybackIntent, currentPlaybackIntent, isPlaybackIntent } from '../playback-intent.js';
import { playAlbumCollection, advanceQueue, deactivateQueue } from '../queue.js';
export { playAlbumCollection } from '../queue.js';
import { API } from '../api.js';
import { state } from '../state.js';
import { currentElement, selectByElement } from '../selection.js';
import {
  currentMediaId,
  currentMediaInfo,
  hasLoadedMedia,
  isPlaybackActive,
  seekRelative,
  selectAndPlay,
} from '../player.js';
import {
  hasActiveFilters,
  loadMedia,
  mediaWithBrowsePath,
} from './media-grid.js';

let isShuffleCallback = () => false;
let playRevision = 0;
export function initPlaybackNav({ isShuffle } = {}) {
  isShuffleCallback = isShuffle || (() => false);
  document.addEventListener('navigation:restore', () => { playRevision++; });
  document.addEventListener('library:navigating', () => { playRevision++; });
}

// Sets browse: when the sequence started from audio, skip images (B3 / §15).
function skipsImagesFrom(media) {
  return media?.type === 'audio';
}

function cardMedia(card) {
  return state.media.find((m) => String(m.id) === String(card.dataset.id));
}

function filterPlayableMedia(media, skipImages) {
  return skipImages ? media.filter((m) => m.type !== 'image') : media;
}

function filterPlayableCards(cards, skipImages) {
  if (!skipImages) return cards;
  return cards.filter((card) => {
    const media = cardMedia(card);
    return media && media.type !== 'image';
  });
}

async function playItem(media, index) {
  deactivateQueue();
  const revision = ++playRevision;
  const intent = beginPlaybackIntent();
  let position = 0;
  try {
    const progress = (await API.mediaDetail(media.id))?.progress;
    position = progress?.finished ? 0 : (progress?.position_seconds ?? 0);
  } catch {}
  if (revision !== playRevision || !isPlaybackIntent(intent)) return false;
  selectAndPlay(media, index, position);
  return true;
}

export async function playSelected() {
  const selected=currentElement();
  if(state.music?.search && selected?.dataset.searchKind==='song') {
    const index=(state.searchSongs || []).findIndex(song=>String(song.id)===selected.dataset.id);
    if(index>=0) await playAlbumCollection(state.searchSongs,index,'Search results');
    return;
  }
  if (state.music?.album || state.music?.playlist) { const i=Number(currentElement()?.dataset.index); if (Number.isInteger(i)) await playAlbumCollection(state.media,i); return; }
  let el = currentElement();
  if (!el) {
    el = document.querySelector('#media-grid .media-card[data-id], #media-grid .media-row[data-id]');
    if (el) selectByElement(el);
  }
  if (!el) return;
  const idx = parseInt(el.dataset.index, 10);
  const media = state.media[idx];
  if (!media) return;
  await playItem(media, idx);
}

// playMediaById plays a media item from the loaded list by ID, selecting its
// card when the current page shows it. Returns false when it is not loaded.
export async function playMediaById(id) {
  // Subscription episode pages leave null holes for undownloaded rows so
  // card indices stay aligned; skip those slots when resolving by id.
  const idx = state.media.findIndex((m) => m && String(m.id) === String(id));
  if (idx < 0) return false;
  // Folder cards also use data-index, so select by media ID.
  const card = document.querySelector(`#media-grid .media-card[data-id="${CSS.escape(String(id))}"]`);
  if (card) selectByElement(card);
  return playItem(state.media[idx], idx);
}

export async function playRandom() {
  if (!state.media.length) return;
  const idx = Math.floor(Math.random() * state.media.length);
  const media = state.media[idx];
  if (!media) return;
  await playItem(media, idx);
}

function visiblePlayableCards() {
  return Array.from(document.querySelectorAll('#media-grid .media-card[data-id], #media-grid .media-row[data-id]')).filter(row=>!state.music?.search || row.dataset.searchKind==='file');
}

function selectedPlayableIndex(cards, preferCurrent) {
  const currentID = String(currentMediaId() || '');
  if (preferCurrent && currentID) {
    const idx = cards.findIndex((el) => el.dataset.id === currentID);
    if (idx >= 0) return idx;
  }
  const selected = currentElement();
  const idx = selected ? cards.indexOf(selected) : -1;
  return idx >= 0 ? idx : -1;
}

export async function navigatePlayable(delta, options = {}) {
  const intent = currentPlaybackIntent();
  if (await advanceQueue(delta, options)) return;
  if (!isPlaybackIntent(intent)) return;
  const skipImages = skipsImagesFrom(currentMediaInfo());
  const cards = filterPlayableCards(visiblePlayableCards(), skipImages);
  const active = options.forcePlay || isPlaybackActive();
  const base = selectedPlayableIndex(cards, active);

  if (cards.length && !(active && base < 0)) {
    const atForwardEdge = delta > 0 && base === cards.length - 1;
    const atBackwardEdge = delta < 0 && base === 0;
    if (!((atForwardEdge || atBackwardEdge) && canTraverseBrowseFolders())) {
      // Single-page wrap stays among playable kinds; at the last audio item
      // with nowhere else to go this replays the first audio, never an image.
      const nextIdx = base >= 0
        ? (base + delta + cards.length) % cards.length
        : (delta > 0 ? 0 : cards.length - 1);
      const target = cards[nextIdx];
      selectByElement(target);
      if (active) await playSelected();
      return;
    }
  }

  const crossFolderTarget = await findCrossFolderPlayable(delta, active, skipImages);
  if (!crossFolderTarget) return;
  await openPlayableTarget(crossFolderTarget, active);
}

function canTraverseBrowseFolders() {
  return !!state.selectedSetId && state.selectedSetIds.length === 1 && !isShuffleCallback() && !hasActiveFilters();
}

async function findCrossFolderPlayable(delta, preferCurrent, skipImages) {
  if (!canTraverseBrowseFolders()) return null;
  const current = currentPlaybackCandidate(preferCurrent);
  if (!current) return null;
  const currentPath = current.browse_path ?? state.folderPath ?? '';
  const currentData = await browsePath(currentPath);
  const media = browsePlayableMedia(currentData, currentPath, skipImages);
  const idx = media.findIndex((m) => String(m.id) === String(current.id));

  if (idx >= 0) {
    const nextInFolder = idx + delta;
    if (nextInFolder >= 0 && nextInFolder < media.length) {
      return { path: currentPath, media: media[nextInFolder] };
    }
  }

  return delta > 0
    ? findAfterFolder(currentPath, skipImages)
    : findBeforeFolder(currentPath, skipImages);
}

function currentPlaybackCandidate(preferCurrent) {
  const playing = currentMediaInfo();
  if (preferCurrent && playing) return playing;
  const selected = currentElement();
  if (selected?.dataset?.id) {
    const selectedMedia = state.media.find((m) => String(m.id) === selected.dataset.id);
    if (selectedMedia) return selectedMedia;
  }
  return playing;
}

async function findAfterFolder(path, skipImages) {
  if (!path) return firstPlayableInFolder('', skipImages);
  let childPath = normalizePath(path);
  while (childPath) {
    const parent = parentPath(childPath);
    const name = baseName(childPath);
    const data = await browsePath(parent);
    const items = browseItems(data, parent, skipImages);
    const idx = items.findIndex((item) => item.type === 'folder' && item.name === name);
    for (const item of items.slice(idx + 1)) {
      const target = item.type === 'media'
        ? { path: parent, media: item.media }
        : await firstPlayableInFolder(joinPath(parent, item.name), skipImages);
      if (target) return target;
    }
    childPath = parent;
  }
  return firstPlayableInFolder('', skipImages);
}

async function findBeforeFolder(path, skipImages) {
  if (!path) return lastPlayableInFolder('', skipImages);
  let childPath = normalizePath(path);
  while (childPath) {
    const parent = parentPath(childPath);
    const name = baseName(childPath);
    const data = await browsePath(parent);
    const items = browseItems(data, parent, skipImages);
    const idx = items.findIndex((item) => item.type === 'folder' && item.name === name);
    for (const item of items.slice(0, Math.max(0, idx)).reverse()) {
      const target = item.type === 'media'
        ? { path: parent, media: item.media }
        : await lastPlayableInFolder(joinPath(parent, item.name), skipImages);
      if (target) return target;
    }
    childPath = parent;
  }
  return lastPlayableInFolder('', skipImages);
}

async function firstPlayableInFolder(path, skipImages) {
  const data = await browsePath(path);
  for (const folder of data.folders || []) {
    const target = await firstPlayableInFolder(joinPath(path, folder.name), skipImages);
    if (target) return target;
  }
  const media = browsePlayableMedia(data, path, skipImages);
  if (media.length) return { path: data.current_path || path, media: media[0] };
  return null;
}

async function lastPlayableInFolder(path, skipImages) {
  const data = await browsePath(path);
  const media = browsePlayableMedia(data, path, skipImages);
  if (media.length) return { path: data.current_path || path, media: media[media.length - 1] };
  const folders = data.folders || [];
  for (const folder of folders.slice().reverse()) {
    const target = await lastPlayableInFolder(joinPath(path, folder.name), skipImages);
    if (target) return target;
  }
  return null;
}

function browseItems(data, path, skipImages) {
  const folders = (data.folders || []).map((folder) => ({ type: 'folder', name: folder.name }));
  const media = browsePlayableMedia(data, path, skipImages)
    .map((m) => ({ type: 'media', media: m }));
  return folders.concat(media);
}

async function browsePath(path) {
  return API.browse(state.selectedSetId, path || '');
}

function normalizePath(path) {
  return (path || '').split('/').filter(Boolean).join('/');
}

function parentPath(path) {
  const normalized = normalizePath(path);
  const idx = normalized.lastIndexOf('/');
  return idx >= 0 ? normalized.slice(0, idx) : '';
}

function baseName(path) {
  const normalized = normalizePath(path);
  const idx = normalized.lastIndexOf('/');
  return idx >= 0 ? normalized.slice(idx + 1) : normalized;
}

function joinPath(parent, child) {
  const cleanParent = normalizePath(parent);
  const cleanChild = normalizePath(child);
  return cleanParent ? `${cleanParent}/${cleanChild}` : cleanChild;
}

async function openPlayableTarget(target, play) {
  state.folderPath = target.path || '';
  await loadMedia();
  const card = document.querySelector(`#media-grid [data-id="${target.media.id}"]`);
  if (card) selectByElement(card);
  const idx = state.media.findIndex((m) => String(m.id) === String(target.media.id));
  const media = idx >= 0 ? state.media[idx] : { ...target.media, browse_path: target.path || '' };
  if (play) {
    await playItem(media, idx >= 0 ? idx : 0);
  }
}

export function seekByKeyboard(direction, repeated) {
  if (!hasLoadedMedia()) return false;
  if (!document.fullscreenElement) return false;
  const step = repeated ? 15 : 5;
  return seekRelative(direction * step);
}

function browsePlayableMedia(data, path, skipImages) {
  return filterPlayableMedia(
    mediaWithBrowsePath(data.media || [], data.current_path || path),
    skipImages,
  );
}
