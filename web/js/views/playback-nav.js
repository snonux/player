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

export function initPlaybackNav({ isShuffle } = {}) {
  isShuffleCallback = isShuffle || (() => false);
}

export async function playSelected() {
  let el = currentElement();
  if (!el) {
    el = document.querySelector('#media-grid .media-card[data-id], #media-grid .media-row[data-id]');
    if (el) selectByElement(el);
  }
  if (!el) return;
  const idx = parseInt(el.dataset.index, 10);
  const media = state.media[idx];
  if (!media) return;
  try {
    const detail = await API.mediaDetail(media.id);
    const resumeFrom = detail?.progress?.position_seconds ?? 0;
    selectAndPlay(media, idx, resumeFrom);
  } catch {
    selectAndPlay(media, idx, 0);
  }
}

export async function playRandom() {
  if (!state.media.length) return;
  const idx = Math.floor(Math.random() * state.media.length);
  const media = state.media[idx];
  if (!media) return;
  try {
    const detail = await API.mediaDetail(media.id);
    const resumeFrom = detail?.progress?.position_seconds ?? 0;
    selectAndPlay(media, idx, resumeFrom);
  } catch {
    selectAndPlay(media, idx, 0);
  }
}

function visiblePlayableCards() {
  return Array.from(document.querySelectorAll('#media-grid .media-card[data-id], #media-grid .media-row[data-id]'));
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
  const cards = visiblePlayableCards();
  const active = options.forcePlay || isPlaybackActive();
  const base = selectedPlayableIndex(cards, active);

  if (cards.length && !(active && base < 0)) {
    const atForwardEdge = delta > 0 && base === cards.length - 1;
    const atBackwardEdge = delta < 0 && base === 0;
    if (!((atForwardEdge || atBackwardEdge) && canTraverseBrowseFolders())) {
      const nextIdx = base >= 0
        ? (base + delta + cards.length) % cards.length
        : (delta > 0 ? 0 : cards.length - 1);
      const target = cards[nextIdx];
      selectByElement(target);
      if (active) await playSelected();
      return;
    }
  }

  const crossFolderTarget = await findCrossFolderPlayable(delta, active);
  if (!crossFolderTarget) return;
  await openPlayableTarget(crossFolderTarget, active);
}

function canTraverseBrowseFolders() {
  return !!state.selectedSetId && state.selectedSetIds.length === 1 && !isShuffleCallback() && !hasActiveFilters();
}

async function findCrossFolderPlayable(delta, preferCurrent) {
  if (!canTraverseBrowseFolders()) return null;
  const current = currentPlaybackCandidate(preferCurrent);
  if (!current) return null;
  const currentPath = current.browse_path ?? state.folderPath ?? '';
  const currentData = await browsePath(currentPath);
  const media = mediaWithBrowsePath(currentData.media || [], currentPath);
  const idx = media.findIndex((m) => String(m.id) === String(current.id));

  if (idx >= 0) {
    const nextInFolder = idx + delta;
    if (nextInFolder >= 0 && nextInFolder < media.length) {
      return { path: currentPath, media: media[nextInFolder] };
    }
  }

  return delta > 0
    ? findAfterFolder(currentPath)
    : findBeforeFolder(currentPath);
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

async function findAfterFolder(path) {
  if (!path) return firstPlayableInFolder('');
  let childPath = normalizePath(path);
  while (childPath) {
    const parent = parentPath(childPath);
    const name = baseName(childPath);
    const data = await browsePath(parent);
    const items = browseItems(data, parent);
    const idx = items.findIndex((item) => item.type === 'folder' && item.name === name);
    for (const item of items.slice(idx + 1)) {
      const target = item.type === 'media'
        ? { path: parent, media: item.media }
        : await firstPlayableInFolder(joinPath(parent, item.name));
      if (target) return target;
    }
    childPath = parent;
  }
  return firstPlayableInFolder('');
}

async function findBeforeFolder(path) {
  if (!path) return lastPlayableInFolder('');
  let childPath = normalizePath(path);
  while (childPath) {
    const parent = parentPath(childPath);
    const name = baseName(childPath);
    const data = await browsePath(parent);
    const items = browseItems(data, parent);
    const idx = items.findIndex((item) => item.type === 'folder' && item.name === name);
    for (const item of items.slice(0, Math.max(0, idx)).reverse()) {
      const target = item.type === 'media'
        ? { path: parent, media: item.media }
        : await lastPlayableInFolder(joinPath(parent, item.name));
      if (target) return target;
    }
    childPath = parent;
  }
  return lastPlayableInFolder('');
}

async function firstPlayableInFolder(path) {
  const data = await browsePath(path);
  for (const folder of data.folders || []) {
    const target = await firstPlayableInFolder(joinPath(path, folder.name));
    if (target) return target;
  }
  const media = mediaWithBrowsePath(data.media || [], data.current_path || path);
  if (media.length) return { path: data.current_path || path, media: media[0] };
  return null;
}

async function lastPlayableInFolder(path) {
  const data = await browsePath(path);
  const media = mediaWithBrowsePath(data.media || [], data.current_path || path);
  if (media.length) return { path: data.current_path || path, media: media[media.length - 1] };
  const folders = data.folders || [];
  for (const folder of folders.slice().reverse()) {
    const target = await lastPlayableInFolder(joinPath(path, folder.name));
    if (target) return target;
  }
  return null;
}

function browseItems(data, path) {
  const folders = (data.folders || []).map((folder) => ({ type: 'folder', name: folder.name }));
  const media = mediaWithBrowsePath(data.media || [], data.current_path || path)
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
    try {
      const detail = await API.mediaDetail(media.id);
      const resumeFrom = detail?.progress?.position_seconds ?? 0;
      selectAndPlay(media, idx >= 0 ? idx : 0, resumeFrom);
    } catch {
      selectAndPlay(media, idx >= 0 ? idx : 0, 0);
    }
  }
}

export function seekByKeyboard(direction, repeated) {
  if (!hasLoadedMedia()) return false;
  if (!document.fullscreenElement) return false;
  const step = repeated ? 15 : 5;
  return seekRelative(direction * step);
}
