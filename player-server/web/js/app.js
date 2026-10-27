import { offlineMusicMode } from './offline-context.js';
import { initOfflineMusicApp } from './offline-browse.js';
import { initLanguage } from './i18n.js';
import { initOfflinePinLibrary } from './offline-pin-library.js';
import { mountMusicPresence } from './music-presence.js';
import { initRadio } from './radio.js';
import { stopRenderer } from './renderer.js';
import { setListeningProgressAccount } from './listening-progress.js';
import { initGlobalSyncControls } from './global-sync-controls.js';
import { setPodcastAccount } from './podcast-episode-state.js';
import { initListeningHistory } from './listening-history.js';
import { initSongNotifications, clearSongNotifications } from './song-notifications.js';
import { initPlaybackCache, releasePlaybackCache, setPlaybackCacheAccount } from './playback-cache.js';
import { initNavigationPreferences, readNavigationPreferences, coldLanding } from './navigation-preferences.js';
import { nextMusicSearch, musicQuery, captureSearchView, restoreSearchView, hasSearchView, forgetSearchView } from './music-search.js';
import { cancelCollectionSelection } from './collection-actions.js';
import { queueSelected } from './queue-actions.js';
import { initQueue, restoreQueue, queueState, cycleQueueRepeat } from './queue.js';
import { initQueuePanel, isQueueFocused, queueKey, toggleQueue } from './queue-panel.js';
import { initMusicCatalog, renderMusic, backMusic, openAlbums, openAlbum, openSongAlbum } from './views/music-catalog.js';
import { API } from './api.js';
import { initKeyboard } from './keyboard.js';
import { initPictureLayout } from './views/picture-layout.js';
import { initThumbnailSize } from './views/thumbnail-size.js';
import { initSelection, selectByElement, currentElement, focusSelectedCard, navUp, navDown, navLeft, navRight } from './selection.js';
import {
  initPlayer,
  togglePlay,
  toggleFullscreen,
  toggleMinimize,
  toggleDetach,
  escapePlayerView,
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
  panImage,
  resetZoom,
} from './player.js';
import { initSearch, parseQuery, showSearchHelp, cancelPendingSearch, hideSearch } from './search.js';
import { initShuffle, enable as enableShuffle, isOn as isShuffle, revision as shuffleRevision, setOn as setShuffle } from './shuffle.js';
import { initNavigation } from './navigation.js';
import { initKeyboardAccess } from './keyboard-access.js';
import { safeReturnTo, loginDestination } from './navigation-url.js';
import { initThemes } from './themes.js';
import { initNotes } from './notes.js';
import { initAdmin } from './admin.js';
import { initTrash, moveToTrash } from './trash.js';
import { initModalFocus } from './modal-focus.js';
import { initPodcasts } from './podcasts.js';
import { initPodcastViews, updatePodcastSidebarUI, clearPodcastView } from './podcast-views.js';
import { state, setAuthenticatedUser } from './state.js';
import { initPWA } from './pwa.js';
import { closeAllModals } from './dom.js';
import { toast } from './utils.js';
import { initShareLinkFallback } from './share-link.js';
import { showAdmin, triggerRescan } from './views/admin-status.js';
import { closeSidebar, dismissSidebarAfterNav, focusHelpSearch, initHelp, showSearch, toggleHelp, toggleSidebar } from './views/help.js';
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

let loggingOut = false;
let closeMusicPresence = null;

const pageMap = { '/index.html': 'spa', '/login.html': 'login', '/bootstrap.html': 'bootstrap' };

main();

function main() {
  initKeyboardAccess();
  const page = pageMap[location.pathname] || 'spa';
  if (page === 'login') initLogin();
  else if (page === 'bootstrap') initBootstrap();
  else if (offlineMusicMode()) initOfflineMusicApp();
  else initApp();
  initThemes();
  initLanguage();
}

function initLogin() {
  const form = document.getElementById('login-form');
  const err = document.getElementById('login-error');
  if (!form) return;
  explainLoginAuthMode();
  tryProxySignIn();
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
      location.href = safeReturnTo(new URLSearchParams(location.search).get('return_to'));
    } catch (ex) {
      err.textContent = actionableLoginFailure(ex);
    }
  });
}

async function explainLoginAuthMode() {
  const note = document.getElementById('login-auth-mode');
  if (!note) return;
  try {
    const status = await API.authStatus();
    if (!status?.proxy) return;
    const header = status.proxy_auth?.user_header || 'Remote-User';
    note.textContent = `Reverse-proxy sign-in is enabled (${header}). If you reached this page, the proxy did not map you to a Player account — use password sign-in below, or ask an admin to create a matching username.`;
    note.classList.remove('hidden');
  } catch {
    // Public status is optional; keep the password form usable.
  }
}

async function tryProxySignIn() {
  try {
    // Probe without api()'s 401→login redirect (we are already on login).
    const res = await fetch('/api/config', { credentials: 'include', headers: { Accept: 'application/json' } });
    if (res.ok) {
      location.href = safeReturnTo(new URLSearchParams(location.search).get('return_to'));
    }
  } catch {
    // Not already authenticated via proxy/cookie.
  }
}

function actionableLoginFailure(ex) {
  const message = ex?.message || 'Login failed';
  if (ex?.status === 401 || /invalid credentials/i.test(message)) {
    return 'Username or password is wrong. If your site uses reverse-proxy sign-in, confirm the proxy sends your username and that a matching Player account exists.';
  }
  if (ex?.status === 0 || /failed to fetch|network/i.test(message)) {
    return 'Could not reach the Player server. Check the address and your connection, then try again.';
  }
  return message;
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
      location.href = loginDestination('/login.html', new URLSearchParams(location.search).get('return_to'));
    } catch (ex) {
      err.textContent = ex.message || 'Bootstrap failed';
    }
  });
}

async function initApp() {
  initPlayer({
    personalActions: true,
    cast: true,
    onNext: (options) => navigatePlayable(options?.delta ?? 1, options),
    onPrevious: (options) => navigatePlayable(options?.delta ?? -1, options),
  });
  initSelection();
  initMusicCatalog(loadMedia);
  let navigatedDuringStartup = false;
  window.addEventListener('popstate', () => { navigatedDuringStartup = true; }, { once: true });
  const initialCollection = coldLanding(readNavigationPreferences(), {
    search: location.search, hash: location.hash,
    historyNavigation: performance.getEntriesByType('navigation')[0]?.type === 'back_forward',
  });
  const openCollection = name => openAlbums('', '', name === 'artists', name === 'playlists');
  initNavigationPreferences(openCollection);
  initListeningHistory();
  initPlaybackCache(() => { const q = queueState(); return q.entries[q.index + 1]?.media; });
  initOfflinePinLibrary();
  document.addEventListener('music:song-info', toggleMediaInfo);
  document.addEventListener('music:open-song-album', e => openSongAlbum(e.detail));
  initSearch({
    onChange: (q) => {
      const parsed = parseQuery(q);
      captureSearchView();
      const music=musicQuery(q,parsed,Boolean(state.music));
      if(music) {state.music=nextMusicSearch(state.music,music);loadMedia();return;}
      state.music = null;
      state.virtualSet = '';
      clearPodcastView();
      state.podcastFeedKey = '';
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
  let searchNavigate=false;
  document.addEventListener('search:open', captureSearchView);
  document.addEventListener('search:restore', async () => {cancelPendingSearch();hideSearch();searchNavigate=false; if(!await restoreSearchView(loadMedia) && state.music?.search) {state.music=null;await loadMedia();}});
  document.addEventListener('search:forget', forgetSearchView);
  document.addEventListener('navigation:restore', forgetSearchView);
  document.addEventListener('search:play-selected', () => void playSelected());
  document.addEventListener('search:navigate-results', () => {searchNavigate=true;});
  document.addEventListener('library:navigated', () => {if(searchNavigate){searchNavigate=false;navDown();if(!focusSelectedCard())document.querySelector('#media-grid [data-retry], #media-grid [data-search-back], #media-grid [data-music-back]')?.focus();}});
  initShuffle({ onChange: () => loadMedia() });
  initPlaybackNav({ isShuffle });
  initQueue();
  initQueuePanel();
  initSets({
    onLoadMedia: () => {
      forgetSearchView();
      state.music = null;
      state.virtualSet = '';
      state.podcastFeedKey = '';
      syncChromeVirtualSets();
      loadMedia();
    },
  });
  initMediaGrid({
    renderMusic,
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
    moveToTrash,
    onVirtualSetCleared: syncChromeVirtualSets,
  });
  initPodcastViews({
    onLoadMedia: () => {
      forgetSearchView();
      syncChromeVirtualSets();
      loadMedia();
    },
    playMediaById,
  });
  initKeyboard({
    ...keyboardHandlers(),
    isQueueFocused, queueKey, toggleQueue, cycleQueueRepeat,
    queueNext: () => queueSelected('next'), queueAppend: () => queueSelected('append'),
    radio: () => document.getElementById('radio-toggle')?.click(),
    togglePictureLayout: initPictureLayout(document.getElementById('media-grid')),
    ...initThumbnailSize(document.getElementById('media-grid')),
  });
  initNotes((status, message) => toast(status === 'error' ? message || 'Could not update note.' : status === 'deleted' ? 'Note deleted' : 'Note saved', status === 'error' ? 'error' : 'info'));
  initAdmin();
  initTrash();
  initRadio();
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
      return loadMedia();
    },
  });
  initChrome();

  try {
    const [cfg, sets] = await Promise.all([
      API.config().catch(() => null),
      API.sets(),
    ]);
    if (loggingOut) return;
    setAuthenticatedUser(cfg?.user_id);
    closeMusicPresence?.();
    closeMusicPresence = null;
    const presenceHost = document.querySelector('.main-content');
    if (state.user && presenceHost) closeMusicPresence = mountMusicPresence(presenceHost);
    setPlaybackCacheAccount(cfg?.user_id);
    setListeningProgressAccount(cfg?.user_id);
    setPodcastAccount(cfg?.user_id);
    initSongNotifications(cfg?.user_id);
    setMediaPageSize(cfg?.media_page_size);
    state.sets = (sets || []).slice().sort((a, b) => a.name.localeCompare(b.name));
    API.users().then(() => {
      state.isAdmin = true;
      showAdmin();
    }).catch(() => {});
    renderSets();
    const navigation = initNavigation({ isShuffle, setShuffle, syncChrome: syncChromeVirtualSets });
    await restoreQueue(cfg?.user_id);
    void initGlobalSyncControls()?.syncNow();
    if (queueState().restored) toast('Queue restored, paused. Open Now Playing to resume.', 'info');
    await navigation.restore();
    if (!navigatedDuringStartup && initialCollection !== 'library' && !location.search && !location.hash) openCollection(initialCollection);
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
      // Media info returns to its actual opener through modal-focus. Do not
      // move focus to a grid card hidden behind the fullscreen image.
      if (isMediaInfoOpen()) {
        e?.preventDefault();
        closeMediaInfo();
        return;
      }
      // Let modal-focus restore the help opener, including image controls.
      const openDialogs = document.querySelectorAll('.modal-overlay.open');
      if (openDialogs.length === 1 && openDialogs[0].id === 'help-modal') {
        openDialogs[0].classList.remove('open');
        return;
      }
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
      if(hasSearchView() && (!state.music || state.music.search)) {if(!escapeMediaView())document.dispatchEvent(new Event('search:restore'));return;}
      cancelTagsLoad();
      if (!escapeMediaView()) cancelCollectionSelection();
    },
    shuffle: () => {
      enableShuffle();
      loadMedia();
    },
    share: () => shareSelected(),
    search: () => showSearch(),
    helpSearch: focusHelpSearch,
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
    backspace: (e) => {
      e.preventDefault();
      if (!escapeMediaView() && !backMusic()) navigateBack();
    },
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
    isLightboxOpen: () => playerIsImageMode() && document.getElementById('player')?.classList.contains('open') && !document.getElementById('player')?.classList.contains('minimized') && !document.querySelector('.modal-overlay.open'),
    lightboxPrev: () => navigateImage(-1, { manual: true }),
    lightboxNext: () => navigateImage(1, { manual: true }),
    closeLightbox: () => escapeMediaView(),
    toggleSlideshow: () => toggleSlideshow(),
    imageFullscreenNavigate: (delta) => {
      if (!document.fullscreenElement || !playerIsImageMode()) return false;
      navigatePlayable(delta, { forcePlay: true });
      return true;
    },
    panImage: (dx, dy) => panImage(dx, dy),
    resetZoom: () => resetZoom(),
    zoomIn: () => {
      if (playerIsImageMode()) playerZoomIn();
    },
    zoomOut: () => {
      if (playerIsImageMode()) playerZoomOut();
    },
  };
}

function escapeMediaView() {
  const wasFullscreen = Boolean(document.fullscreenElement);
  const escaped = escapePlayerView();
  if (escaped && !wasFullscreen) focusSelectedCard();
  return escaped;
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
  if (el.dataset.collectionKey) { el.querySelector('.collection-open')?.click(); return; }
  if (el.dataset.playlist) { el.click(); return; }
  if (el.dataset.artistCard) { el.click(); return; }
  if (el.dataset.album) { openAlbum(el.dataset.album); return; }
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
  forgetSearchView();
  state.music = null;
  if (!Number.isFinite(id)) return;
  state.virtualSet = '';
  state.podcastFeedKey = '';
  state.selectedSetId = id;
  state.selectedSetIds = [id];
  state.folderPath = '';
  state.mediaPage = 0;
  updateSetRowsUI();
  syncChromeVirtualSets();
  dismissSidebarAfterNav();
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
  state.music = null;
  state.virtualSet = state.virtualSet === 'in-progress' ? '' : 'in-progress';
  state.podcastFeedKey = '';
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
  updatePodcastSidebarUI();
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
    closeSidebar();
  });

  document.getElementById('logout-btn')?.addEventListener('click', async () => {
    loggingOut = true;
    closeMusicPresence?.();
    closeMusicPresence = null;
    setAuthenticatedUser(null);
    try {
      try { await stopRenderer(); } catch {}
      setListeningProgressAccount(null);
      setPodcastAccount(null);
      clearSongNotifications();
      releasePlaybackCache();
      const result = await API.logout();
      location.href = result?.logout_url || '/login.html';
    } catch {}
  });
}
