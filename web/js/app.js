import { API } from './api.js';
import { initKeyboard } from './keyboard.js';
import { initSelection, selectByElement, currentElement, navUp, navDown, navLeft, navRight } from './selection.js';
import {
  initPlayer,
  togglePlay,
  toggleFullscreen,
  toggleMinimize,
  toggleDetach,
  exitFullscreenIfNeeded,
  toggleCrop,
  shiftCropPosition,
  cycleCropPosition,
  currentMediaInfo,
  hasLoadedMedia,
  seekPercent,
  stopAndClose,
  zoomIn as playerZoomIn,
  zoomOut as playerZoomOut,
  isImageMode as playerIsImageMode,
} from './player.js';
import { initSearch, parseQuery, showSearchHelp } from './search.js';
import { initShuffle, enable as enableShuffle, isOn as isShuffle, revision as shuffleRevision } from './shuffle.js';
import { initThemes } from './themes.js';
import { initNotes } from './notes.js';
import { initAdmin } from './admin.js';
import { initPodcasts } from './podcasts.js';
import { state } from './state.js';
import { initPWA } from './pwa.js';
import { closeAllModals } from './dom.js';
import { toast } from './utils.js';
import { showAdmin } from './views/admin-status.js';
import { initHelp, showSearch, toggleHelp, toggleSidebar } from './views/help.js';
import {
  initMediaGrid,
  loadMedia,
  enterFolder,
  navigateBack,
  setMediaPageSize,
} from './views/media-grid.js';
import {
  downloadSelected,
  openNotesForSelected,
  regenThumb,
  selectedMediaId,
  shareSelected,
  toggleFavorite,
} from './views/media-actions.js';
import {
  closeMediaInfo,
  initMediaInfo,
  isMediaInfoOpen,
  scrollMediaInfo,
  toggleMediaInfo,
} from './views/media-info.js';
import {
  navigatePlayable,
  initPlaybackNav,
  playRandom,
  playSelected,
  seekByKeyboard,
} from './views/playback-nav.js';
import {
  initSets,
  renderSets,
  selectSetByHotkey,
  setSetByDelta,
  toggleSetSelection,
  updateSetRowsUI,
} from './views/sets.js';
import {
  copySelectedShare,
  deleteSelectedShare,
  initShares,
  isSharesOpen,
  sharesNav,
  toggleShares,
} from './views/shares.js';
import { initTags, openTagsForElement, openTagsForSelected } from './views/tags.js';
import { initUpload, showUpload } from './views/upload.js';

const pageMap = { '/index.html': 'spa', '/login.html': 'login', '/bootstrap.html': 'bootstrap' };

main();

function main() {
  const page = pageMap[location.pathname] || 'spa';
  if (page === 'login') initLogin();
  else if (page === 'bootstrap') initBootstrap();
  else initApp();
  initThemes();
}

function initLogin() {
  const form = document.getElementById('login-form');
  const err = document.getElementById('login-error');
  if (!form) return;
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    err.textContent = '';
    const fd = new FormData(form);
    const username = fd.get('username');
    const password = fd.get('password');
    if (!username || !password) {
      err.textContent = 'Please fill in all fields.';
      return;
    }
    try {
      await API.login(username, password);
      location.href = '/';
    } catch (ex) {
      err.textContent = ex.message || 'Login failed';
    }
  });
}

function initBootstrap() {
  const form = document.getElementById('bootstrap-form');
  const err = document.getElementById('bootstrap-error');
  if (!form) return;
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    err.textContent = '';
    const fd = new FormData(form);
    const username = fd.get('username');
    const password = fd.get('password');
    const confirm = fd.get('password-confirm');
    if (!username || !password || !confirm) {
      err.textContent = 'All fields are required.';
      return;
    }
    if (password !== confirm) {
      err.textContent = 'Passwords do not match.';
      return;
    }
    try {
      await API.bootstrap(username, password);
      location.href = '/login.html';
    } catch (ex) {
      err.textContent = ex.message || 'Bootstrap failed';
    }
  });
}

async function initApp() {
  initPlayer({
    onNext: (options) => navigatePlayable(options?.delta ?? 1, options),
    onPrevious: (options) => navigatePlayable(options?.delta ?? -1, options),
  });
  initSelection();
  initSearch({
    onChange: (q) => {
      const parsed = parseQuery(q);
      applySearchSet(parsed);
      delete parsed.set;
      Object.assign(state.filters, parsed);
      state.folderPath = '';
      document.dispatchEvent(new CustomEvent('filters:changed'));
      console.log('[search] raw:', q, 'parsed:', parsed, 'filters:', state.filters);
      loadMedia();
    },
    input: document.getElementById('search-input'),
    clearBtn: document.getElementById('search-clear'),
  });
  document.addEventListener('search:navigate-results', () => navDown());
  initShuffle({ onChange: () => loadMedia() });
  initPlaybackNav({ isShuffle });
  initSets({ onLoadMedia: loadMedia });
  initMediaGrid({
    isShuffle,
    shuffleRevision,
    onSetCleared: updateSetRowsUI,
    openNotesForSelected,
    openTagsForElement,
    openSet,
    playSelected,
    regenThumb,
    toggleFavorite,
  });
  initKeyboard(keyboardHandlers());
  initNotes(() => toast('Note saved'));
  initAdmin();
  initPodcasts();
  initPWA();
  initUpload({ onLoadMedia: loadMedia });
  initHelp();
  initShares();
  initMediaInfo();
  initTags({
    onFilterChange: () => {
      document.dispatchEvent(new CustomEvent('filters:changed'));
      loadMedia();
    },
  });
  initChrome();

  try {
    const [cfg, sets] = await Promise.all([
      API.config().catch(() => null),
      API.sets(),
    ]);
    setMediaPageSize(cfg?.media_page_size);
    state.sets = (sets || []).slice().sort((a, b) => a.name.localeCompare(b.name));
    API.users().then(() => {
      state.isAdmin = true;
      showAdmin();
    }).catch(() => {});
    renderSets();
    await loadMedia();
  } catch (err) {
    toast(err.message || 'Error loading sets', 'error');
  }
}

function applySearchSet(parsed) {
  const setQuery = parsed.set?.trim();
  if (!setQuery) return;
  const needle = setQuery.toLowerCase();
  const match = state.sets.find((set) => String(set.id) === setQuery) ||
    state.sets.find((set) => set.name.toLowerCase() === needle) ||
    state.sets.find((set) => set.name.toLowerCase().includes(needle));
  if (!match) return;
  state.selectedSetId = match.id;
  state.selectedSetIds = [match.id];
  updateSetRowsUI();
}

function keyboardHandlers() {
  return {
    navUp: () => navUp(),
    navDown: () => navDown(),
    navLeft: () => navLeft(),
    navRight: () => navRight(),
    seekBackward: (e) => seekByKeyboard(-1, e.repeat),
    seekForward: (e) => seekByKeyboard(1, e.repeat),
    seekPercent: (e, percent) => {
      if (!hasLoadedMedia()) return false;
      if (!document.fullscreenElement) return false;
      return seekPercent(percent);
    },
    nextSet: () => setSetByDelta(1),
    prevSet: () => setSetByDelta(-1),
    selectSetByHotkey: (key) => selectSetByHotkey(key),
    isSidebarOpen: () => document.getElementById('sidebar')?.classList.contains('open'),
    isSidebarFocused: () => {
      const sidebar = document.getElementById('sidebar');
      return sidebar?.contains(document.activeElement);
    },
    toggleSetSelect: () => toggleSetSelection(),
    enter: () => {
      activateGridElement(focusedGridElement() || currentElement() || firstGridElement());
    },
    playPause: () => togglePlay(),
    nextTrack: () => navigatePlayable(1, { forcePlay: true }),
    prevTrack: () => navigatePlayable(-1, { forcePlay: true }),
    playRandom: () => playRandom(),
    mediaInfo: () => toggleMediaInfo(),
    fullscreen: () => toggleFullscreen(),
    toggleMinimize: () => toggleMinimize(),
    toggleCrop: () => toggleCrop(),
    shiftCropPosition: (dx, dy) => shiftCropPosition(dx, dy),
    cycleCropPosition: () => cycleCropPosition(),
    escape: () => {
      exitFullscreenIfNeeded();
      const el = currentElement();
      if (el) el.classList.remove('selected');
      closeAllModals();
    },
    shuffle: () => {
      enableShuffle();
      loadMedia();
    },
    share: () => shareSelected(),
    search: () => showSearch(),
    notes: openNotesForSelected,
    tags: openTagsForSelected,
    toggleDetach: () => toggleDetach(),
    stopAndClose: () => stopAndClose(),
    download: downloadSelected,
    help: toggleHelp,
    backspace: () => { navigateBack(); },
    isMediaInfoOpen,
    closeMediaInfo,
    mediaInfoScroll: scrollMediaInfo,
    sidebar: toggleSidebar,
    upload: () => showUpload(),
    sharesToggle: toggleShares,
    searchHelp: () => showSearchHelp(),
    regenThumbnail: () => {
      const id = selectedMediaId();
      if (id) regenThumb(id);
    },
    isSharesOpen,
    sharesNavUp: () => sharesNav(-1),
    sharesNavDown: () => sharesNav(1),
    sharesCopy: copySelectedShare,
    sharesDelete: deleteSelectedShare,
    isImageMode: () => currentMediaInfo()?.type === 'image',
    imageFullscreenNavigate: (delta) => {
      if (!document.fullscreenElement || !playerIsImageMode()) return false;
      navigatePlayable(delta, { forcePlay: true });
      return true;
    },
    zoomIn: () => {
      if (playerIsImageMode()) playerZoomIn();
    },
    zoomOut: () => {
      if (playerIsImageMode()) playerZoomOut();
    },
  };
}

function focusedGridElement() {
  const active = document.activeElement;
  if (!active || typeof active.closest !== 'function') return null;
  return active.closest('#media-grid .media-card, #media-grid .media-row, #media-grid .folder-card, #media-grid .set-card');
}

function firstGridElement() {
  return document.querySelector('#media-grid .media-card, #media-grid .media-row, #media-grid .folder-card, #media-grid .set-card');
}

function activateGridElement(el) {
  if (!el) return;
  selectByElement(el);
  if (el.classList.contains('set-card')) {
    const id = parseInt(el.dataset.setId, 10);
    openSet(id);
    return;
  }
  if (el.classList.contains('folder-card')) {
    enterFolder(el.dataset.name);
    return;
  }
  const idx = parseInt(el.dataset.index, 10);
  const media = state.media[idx];
  if (media) playSelected();
}

function openSet(id) {
  if (!Number.isFinite(id)) return;
  state.selectedSetId = id;
  state.selectedSetIds = [id];
  state.folderPath = '';
  state.mediaPage = 0;
  updateSetRowsUI();
  loadMedia();
}

function initChrome() {
  document.getElementById('sidebar-toggle')?.addEventListener('click', () => {
    toggleSidebar();
  });

  document.getElementById('menu-close')?.addEventListener('click', () => {
    document.getElementById('sidebar')?.classList.remove('open');
    document.querySelector('.page')?.classList.remove('has-sidebar');
  });

  document.getElementById('logout-btn')?.addEventListener('click', async () => {
    try {
      await API.logout();
      location.href = '/login.html';
    } catch {}
  });
}
