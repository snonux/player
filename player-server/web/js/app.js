import { API } from './api.js';
import { initKeyboard } from './keyboard.js';
import { initSelection, selectByElement, currentElement, focusSelectedCard, navUp, navDown, navLeft, navRight } from './selection.js';
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
  navigateImage,
  toggleSlideshow,
} from './player.js';
import { initSearch, parseQuery, showSearchHelp } from './search.js';
import { initShuffle, enable as enableShuffle, isOn as isShuffle, revision as shuffleRevision } from './shuffle.js';
import { initThemes } from './themes.js';
import { initNotes } from './notes.js';
import { initAdmin } from './admin.js';
import { initModalFocus } from './modal-focus.js';
import { initPodcasts } from './podcasts.js';
import { state } from './state.js';
import { initPWA } from './pwa.js';
import { closeAllModals } from './dom.js';
import { toast } from './utils.js';
import { initShareLinkFallback } from './share-link.js';
import { showAdmin, triggerRescan } from './views/admin-status.js';
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
  markAsFinished,
  markAsNotStarted,
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
  playMediaById,
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
import { cancelTagsLoad, initTags, openTagsForElement, openTagsForSelected } from './views/tags.js';
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
      state.virtualSet = '';
      syncChromeVirtualSets();
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
  initSets({
    onLoadMedia: () => {
      state.virtualSet = '';
      syncChromeVirtualSets();
      loadMedia();
    },
  });
  initMediaGrid({
    isShuffle,
    shuffleRevision,
    onSetCleared: updateSetRowsUI,
    openNotesForSelected,
    openTagsForElement,
    openSet,
    playSelected,
    playMediaById,
    regenThumb,
    markAsFinished: markAsFinishedAndRefresh,
    markAsNotStarted: markAsNotStartedAndRefresh,
    toggleFavorite,
    onVirtualSetCleared: syncChromeVirtualSets,
  });
  initKeyboard(keyboardHandlers());
  initNotes(() => toast('Note saved'));
  initAdmin();
  initModalFocus();
  // Admin actions such as restoring from trash change what the grid shows.
  document.addEventListener('library:changed', () => loadMedia());
  initPodcasts();
  initPWA();
  initUpload({ onLoadMedia: loadMedia });
  initHelp();
  initShares();
  initShareLinkFallback();
  initMediaInfo({
    markAsFinished: markAsFinishedAndRefresh,
    markAsNotStarted: markAsNotStartedAndRefresh,
  });
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
  state.virtualSet = '';
  state.selectedSetId = match.id;
  state.selectedSetIds = [match.id];
  updateSetRowsUI();
}

// True while any modal overlay (help, notes, tags, admin, ...) is shown.
function isDialogOpen() {
  return Boolean(document.querySelector('.modal-overlay.open'));
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
    // True only for a focused set row: Space toggles that set's selection.
    // Sidebar buttons (Close, a row's Regenerate cover) keep Space as
    // play/pause like other buttons outside dialogs.
    isSidebarFocused: () => Boolean(document.activeElement?.matches?.('#sidebar .set-row')),
    // Help is the only dialog open, so ? may close it. Opening help over
    // another dialog would stack it underneath and let Escape close both.
    isOnlyHelpOpen: () => {
      const open = document.querySelectorAll('.modal-overlay.open');
      return open.length === 1 && open[0].id === 'help-modal';
    },
    toggleSetSelect: () => toggleSetSelection(),
    enter: () => {
      activateGridElement(currentElement() || focusedGridElement() || firstGridElement());
    },
    playPause: () => togglePlay(),
    nextTrack: () => navigatePlayable(1, { forcePlay: true }),
    prevTrack: () => navigatePlayable(-1, { forcePlay: true }),
    playRandom: () => playRandom(),
    rescanMedia: () => {
      if (state.isAdmin) triggerRescan();
    },
    mediaInfo: () => toggleMediaInfo(),
    fullscreen: () => toggleFullscreen(),
    toggleMinimize: () => toggleMinimize(),
    toggleCrop: () => toggleCrop(),
    shiftCropPosition: (dx, dy) => shiftCropPosition(dx, dy),
    cycleCropPosition: () => cycleCropPosition(),
    escape: (e) => {
      // With a dialog open, Escape only closes it and keeps the selection.
      // Focus left on a hidden dialog control (or body) moves back to the
      // selected card so j/k and Enter continue from there. A dialog that
      // refuses to close (unsaved notes, running upload) gets its focused
      // field back, since keyboard.js already blurred it.
      if (isDialogOpen()) {
        closeAllModals();
        if (isDialogOpen()) {
          if (e?.target?.closest?.('.modal-overlay.open')) e.target.focus();
          return;
        }
        const active = document.activeElement;
        if (!active || active === document.body || active.closest('.modal-overlay:not(.open)')) {
          focusSelectedCard();
        }
        return;
      }
      cancelTagsLoad();
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
    favorite: () => {
      const el = currentElement();
      if (el?.dataset.id) toggleFavorite(el.dataset.id, el.querySelector('[data-action="favorite"]'));
    },
    // The admin toggle is hidden for non-admins; A does nothing for them.
    admin: () => {
      const toggle = document.getElementById('admin-toggle');
      if (toggle && !toggle.classList.contains('hidden')) toggle.click();
    },
    toggleDetach: () => toggleDetach(),
    stopAndClose: () => stopAndClose(),
    download: downloadSelected,
    help: toggleHelp,
    backspace: () => { navigateBack(); },
    isMediaInfoOpen,
    isModalOpen: isDialogOpen,
    closeMediaInfo,
    mediaInfoScroll: scrollMediaInfo,
    sidebar: toggleSidebar,
    upload: () => showUpload(),
    sharesToggle: toggleShares,
    searchHelp: () => showSearchHelp(),
    regenThumbnail: () => {
      const id = selectedMediaId();
      if (id) regenThumb(id);
      else toast('Select a media item first', 'info');
    },
    isSharesOpen,
    sharesNavUp: () => sharesNav(-1),
    sharesNavDown: () => sharesNav(1),
    sharesCopy: copySelectedShare,
    sharesDelete: deleteSelectedShare,
    isImageMode: () => currentMediaInfo()?.type === 'image',
    isLightboxOpen: () => playerIsImageMode() && document.getElementById('player')?.classList.contains('open') && !document.querySelector('.modal-overlay.open'),
    lightboxPrev: () => navigateImage(-1, { manual: true }),
    lightboxNext: () => navigateImage(1, { manual: true }),
    closeLightbox: () => stopAndClose(),
    toggleSlideshow: () => toggleSlideshow(),
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
  if (el.classList.contains('episode-card')) {
    // Episode cards stand for episodes not yet downloaded; Enter behaves
    // like their Play button (download, then play).
    el.querySelector('[data-action="play"]')?.click();
    return;
  }
  const idx = parseInt(el.dataset.index, 10);
  const media = state.media[idx];
  if (media) playSelected();
}

function openSet(id) {
  if (!Number.isFinite(id)) return;
  state.virtualSet = '';
  state.selectedSetId = id;
  state.selectedSetIds = [id];
  state.folderPath = '';
  state.mediaPage = 0;
  updateSetRowsUI();
  loadMedia();
}

async function markAsFinishedAndRefresh(id) {
  const updated = await markAsFinished(id);
  if (updated && state.virtualSet === 'in-progress') await loadMedia();
  return updated;
}

async function markAsNotStartedAndRefresh(id) {
  const updated = await markAsNotStarted(id);
  if (updated && state.virtualSet === 'in-progress') await loadMedia();
  return updated;
}

function openInProgress() {
  state.virtualSet = state.virtualSet === 'in-progress' ? '' : 'in-progress';
  state.selectedSetId = null;
  state.selectedSetIds = [];
  state.folderPath = '';
  state.mediaPage = 0;
  updateSetRowsUI();
  syncChromeVirtualSets();
  loadMedia();
}

function syncChromeVirtualSets() {
  document.getElementById('in-progress-toggle')?.classList.toggle('active', state.virtualSet === 'in-progress');
}

function initChrome() {
  const shuffleBtn = document.getElementById('shuffle-toggle');
  if (shuffleBtn && !document.getElementById('in-progress-toggle')) {
    const btn = document.createElement('button');
    btn.id = 'in-progress-toggle';
    btn.className = 'icon-btn';
    btn.type = 'button';
    btn.title = 'In Progress';
    btn.setAttribute('aria-label', 'Show in-progress media');
    btn.textContent = '◷';
    shuffleBtn.insertAdjacentElement('afterend', btn);
  }
  document.getElementById('in-progress-toggle')?.addEventListener('click', openInProgress);

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
