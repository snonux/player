import { API } from './api.js';
import { initKeyboard } from './keyboard.js';
import { initSelection, clearSelection, select, selectByElement, next, prev, currentIndex, currentElement, navUp, navDown, navLeft, navRight } from './selection.js';
import { initPlayer, togglePlay, toggleFullscreen, toggleMinimize, toggleDetach, exitFullscreenIfNeeded, currentMediaId, currentMediaInfo, hasLoadedMedia, isPlaybackActive, seekRelative, selectAndPlay, zoomIn as playerZoomIn, zoomOut as playerZoomOut, toggleSlideshow as playerToggleSlideshow, isImageMode as playerIsImageMode } from './player.js';
import { initSearch, focusSearch, trigger as triggerSearch, parseQuery } from './search.js';
import { initShuffle, toggle as toggleShuffle, isOn as isShuffle } from './shuffle.js';
import { initThemes } from './themes.js';
import { initNotes, open as openNotes } from './notes.js';
import { initAdmin } from './admin.js';
import { initPodcasts } from './podcasts.js';
import { state, setMedia } from './state.js';
import { initPWA } from './pwa.js';
import { initLightbox, open as openLightbox, close as closeLightbox, isOpen as isLightboxOpen, next as lightboxNext, prev as lightboxPrev, zoomIn as lightboxZoomIn, zoomOut as lightboxZoomOut, toggleSlideshow as lightboxToggleSlideshow } from './lightbox.js';

const pageMap = { '/index.html': 'spa', '/login.html': 'login', '/bootstrap.html': 'bootstrap' };
let scanProgressTimer = null;

main();

function main() {
  const page = pageMap[location.pathname] || 'spa';
  if (page === 'login') initLogin();
  else if (page === 'bootstrap') initBootstrap();
  else initApp();
  initThemes();
}

// Login page
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
    if (!username || !password) { err.textContent = 'Please fill in all fields.'; return; }
    try {
      await API.login(username, password);
      location.href = '/';
    } catch (ex) {
      err.textContent = ex.message || 'Login failed';
    }
  });
}

// Bootstrap page
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
    if (!username || !password || !confirm) { err.textContent = 'All fields are required.'; return; }
    if (password !== confirm) { err.textContent = 'Passwords do not match.'; return; }
    try {
      await API.bootstrap(username, password);
      location.href = '/login.html';
    } catch (ex) {
      err.textContent = ex.message || 'Bootstrap failed';
    }
  });
}

// SPA
async function initApp() {
  initPlayer({
    onNext: (options) => navigatePlayable(1, options),
    onPrevious: (options) => navigatePlayable(-1, options),
  });
  initSelection();
  initSearch({
    onChange: (q) => {
      const parsed = parseQuery(q);
      Object.assign(state.filters, parsed);
      state.folderPath = '';
      console.log('[search] raw:', q, 'parsed:', parsed, 'filters:', state.filters);
      loadMedia();
    },
    input: document.getElementById('search-input'),
    clearBtn: document.getElementById('search-clear'),
  });
  initShuffle({
    onChange: () => loadMedia(),
  });
  initLightbox({
    onNavigate: (delta) => {
      const images = state.media.filter((m) => m.type === 'image');
      if (!images.length) return;
      // Not needed - lightbox navigates internally
    },
  });
  initKeyboard({
    navUp: () => navUp(),
    navDown: () => navDown(),
    navLeft: () => navLeft(),
    navRight: () => navRight(),
    seekBackward: (e) => seekByKeyboard(-1, e.repeat),
    seekForward: (e) => seekByKeyboard(1, e.repeat),
    nextSet: () => setSetByDelta(1),
    prevSet: () => setSetByDelta(-1),
    isSidebarOpen: () => document.getElementById('sidebar')?.classList.contains('open'),
    isSidebarFocused: () => {
      const sidebar = document.getElementById('sidebar');
      return sidebar?.contains(document.activeElement);
    },
    toggleSetSelect: () => toggleSetSelection(),
    enter: () => {
      const el = currentElement();
      if (!el) return;
      selectByElement(el);
      if (el.classList.contains('folder-card')) {
        enterFolder(el.dataset.name);
      } else {
        const idx = parseInt(el.dataset.index, 10);
        const media = state.media[idx];
        if (media?.type === 'image') {
          openLightbox(state.media, media.id);
        } else {
          playSelected();
        }
      }
    },
    playPause: () => togglePlay(),
    nextTrack: () => navigatePlayable(1),
    prevTrack: () => navigatePlayable(-1),
    mediaInfo: () => toggleMediaInfo(),
    fullscreen: () => toggleFullscreen(),
    toggleMinimize: () => toggleMinimize(),
    escape: () => { exitFullscreenIfNeeded(); closeLightbox(); const el = currentElement(); if (el) el.classList.remove('selected'); closeAllModals(); },
    shuffle: () => { toggleShuffle(); loadMedia(); },
    share: () => shareSelected(),
    search: () => showSearch(),
    notes: openNotesForSelected,
    tags: openTagsForSelected,
    toggleDetach: () => toggleDetach(),
    download: downloadSelected,
    help: toggleHelp,
    backspace: () => { navigateBack(); },
    isMediaInfoOpen,
    closeMediaInfo,
    mediaInfoScroll: scrollMediaInfo,
    sidebar: toggleSidebar,
    upload: () => showUpload(),
    sharesToggle: toggleShares,
    isSharesOpen: () => document.getElementById('shares-modal')?.classList.contains('open'),
    sharesNavUp: () => sharesNav(-1),
    sharesNavDown: () => sharesNav(1),
    sharesCopy: copySelectedShare,
    sharesDelete: deleteSelectedShare,
    isLightboxOpen,
    isImageMode: () => currentMediaInfo()?.type === 'image',
    zoomIn: () => {
      if (isLightboxOpen()) { lightboxZoomIn(); }
      else if (playerIsImageMode()) { playerZoomIn(); }
    },
    zoomOut: () => {
      if (isLightboxOpen()) { lightboxZoomOut(); }
      else if (playerIsImageMode()) { playerZoomOut(); }
    },
    toggleSlideshow: () => {
      if (isLightboxOpen()) { lightboxToggleSlideshow(); }
      else if (playerIsImageMode()) { playerToggleSlideshow(); }
    },
    lightboxNext,
    lightboxPrev,
    closeLightbox,
  });
  initNotes(() => toast('Note saved'));
  initAdmin();
  initPodcasts();
  initPWA();
  initUpload();
  initHelp();
  initShares();
  initMediaInfo();

  // Sidebar close
  document.getElementById('menu-close')?.addEventListener('click', () => {
    document.getElementById('sidebar')?.classList.remove('open');
    document.querySelector('.page')?.classList.remove('has-sidebar');
  });

  document.getElementById('logout-btn')?.addEventListener('click', async () => {
    try { await API.logout(); location.href = '/login.html'; } catch {}
  });

  try {
    const sets = await API.sets();
    state.sets = sets || [];
    // Admin check via admin users endpoint
    API.users().then(() => { state.isAdmin = true; showAdmin(); }).catch(() => {});
    renderSets();
    // Do NOT auto-load a set by default; keep the page blank until the user chooses.
  } catch (err) {
    toast(err.message || 'Error loading sets', 'error');
  }

  // Play on double-click (or open lightbox for images)
  document.getElementById('media-grid')?.addEventListener('dblclick', (e) => {
    const el = e.target.closest('.media-card, .media-row');
    if (!el) return;
    selectByElement(el);
    const idx = parseInt(el.dataset.index, 10);
    const media = state.media[idx];
    if (media?.type === 'image') {
      openLightbox(state.media, media.id);
    } else {
      playSelected();
    }
  });

  // Folder navigation
  document.getElementById('media-grid')?.addEventListener('click', (e) => {
    const folder = e.target.closest('.folder-card');
    if (folder) {
      e.stopPropagation();
      enterFolder(folder.dataset.name);
    }
  });

  // Tags modal close
  document.getElementById('tags-close')?.addEventListener('click', closeTagsModal);
  document.getElementById('tags-modal')?.addEventListener('click', (e) => { if (e.target === document.getElementById('tags-modal')) closeTagsModal(); });
  document.getElementById('tags-add')?.addEventListener('click', addTagForSelected);
  document.getElementById('tags-new')?.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); addTagForSelected(); } });
}

function renderSets() {
  const el = document.getElementById('set-list');
  if (!el) return;
  el.innerHTML = state.sets.map((s) =>
    `<div class="set-row" tabindex="0" role="button" data-id="${s.id}" aria-label="Set ${escapeHtml(s.name)}">
      <span class="set-item flex-1-18" data-id="${s.id}">${escapeHtml(s.name)}</span>
      <button class="icon-btn btn-sm" data-cover-set="${s.id}" title="Regenerate cover">🔄</button>
    </div>`
  ).join('');
  el.querySelectorAll('.set-row').forEach((row) => {
    row.addEventListener('click', (e) => {
      if (e.target.closest('[data-cover-set]')) return;
      e.preventDefault();
      // Single-select on click
      const id = parseInt(row.dataset.id, 10);
      state.selectedSetIds = [id];
      state.selectedSetId = id;
      state.folderPath = '';
      updateSetRowsUI();
      loadMedia();
    });
  });
  el.querySelectorAll('[data-cover-set]').forEach((b) => {
    b.addEventListener('click', async (e) => {
      e.stopPropagation();
      const id = b.dataset.coverSet;
      try { await API.regenCover(id); toast('Cover regenerated'); }
      catch (err) { toast(err.message || 'Cover failed', 'error'); }
    });
  });
  updateSetRowsUI();
}

function updateSetRowsUI() {
  document.querySelectorAll('#set-list .set-row').forEach((row) => {
    const id = parseInt(row.dataset.id, 10);
    const item = row.querySelector('.set-item');
    if (!item) return;
    item.classList.toggle('active', id === state.selectedSetId);
    item.classList.toggle('selected', state.selectedSetIds.includes(id));
  });
}

function toggleSetSelection() {
  const sidebar = document.getElementById('sidebar');
  if (!sidebar) return;
  const focused = document.activeElement.closest('.set-row');
  if (!focused) return;
  const id = parseInt(focused.dataset.id, 10);
  const idx = state.selectedSetIds.indexOf(id);
  if (idx >= 0) {
    state.selectedSetIds.splice(idx, 1);
  } else {
    state.selectedSetIds.push(id);
  }
  // Also sync primary selectedSetId for media loading
  if (state.selectedSetIds.length) {
    state.selectedSetId = state.selectedSetIds[state.selectedSetIds.length - 1];
  } else {
    state.selectedSetId = null;
  }
  state.folderPath = '';
  updateSetRowsUI();
  loadMedia();
}

function setActiveSet(id) {
  document.querySelectorAll('.set-item').forEach((a) => a.classList.toggle('active', parseInt(a.dataset.id, 10) === id));
}

function setSetByDelta(dx) {
  if (!state.sets.length) return;
  let idx = state.sets.findIndex((s) => s.id === state.selectedSetId);
  if (idx < 0) idx = 0;
  const nextIdx = (idx + dx + state.sets.length) % state.sets.length;
  state.selectedSetId = state.sets[nextIdx].id;
  state.selectedSetIds = [state.selectedSetId];
  state.folderPath = '';
  setActiveSet(state.selectedSetId);
  loadMedia();
}

async function loadMedia() {
  const grid = document.getElementById('media-grid');
  const hint = document.getElementById('empty-hint');
  if (!grid) return;
  grid.classList.remove('hidden');
  hint?.classList.add('hidden');
  grid.innerHTML = '<p class="text-muted text-sm grid-full">Loading...</p>';

  const resultCount = document.getElementById('result-count');
  const breadcrumb = document.getElementById('breadcrumb-bar');

  try {
    const setIds = state.selectedSetIds.length > 1 ? state.selectedSetIds.join(',') : '';
    const singleSetId = state.selectedSetIds.length === 1 ? state.selectedSetIds[0] : null;

    // When viewing a single set with no filters/search, use the browse endpoint
    // so that subfolders are presented as folders to navigate into.
    if (singleSetId && !setIds && !isShuffle() && !hasActiveFilters()) {
      const data = await API.browse(singleSetId, state.folderPath);
      updateBreadcrumb(data.current_path);
      setMedia(mediaWithBrowsePath(data.media || [], data.current_path || ''));
      renderBrowse(data);
      const total = (data.media?.length || 0) + (data.folders?.length || 0);
      resultCount.textContent = `${total} items`;
    } else {
      breadcrumb?.classList.add('hidden');
      const sort = isShuffle() ? 'random' : (state.filters.sort || 'name');
      const params = {
        set_id: setIds ? '' : String(singleSetId || state.selectedSetId || ''),
        set_ids: setIds,
        type: state.filters.type,
        search: state.filters.search,
        favorites: state.filters.favorites ? 'true' : '',
        tags: state.filters.tags || '',
        min_duration: state.filters.minDuration ? String(parseFloat(state.filters.minDuration) * 60) : '',
        max_duration: state.filters.maxDuration ? String(parseFloat(state.filters.maxDuration) * 60) : '',
        filesize_min: state.filters.minFileSize ? String(parseInt(state.filters.minFileSize, 10) * 1024 * 1024) : '',
        filesize_max: state.filters.maxFileSize ? String(parseInt(state.filters.maxFileSize, 10) * 1024 * 1024) : '',
        sort,
        limit: '1000',
      };
      const data = await API.media(params);
      const list = Array.isArray(data) ? data : data?.media || [];
      setMedia(list);
      renderGrid(list);
      resultCount.textContent = `${list.length} items`;
    }
  } catch (err) {
    grid.innerHTML = `<p class="error-message grid-full">${escapeHtml(err.message)}</p>`;
  }
}

function updateBreadcrumb(currentPath) {
  const el = document.getElementById('breadcrumb-bar');
  if (!el) return;
  const setName = state.sets.find((s) => s.id === state.selectedSetId)?.name || 'Set';
  const parts = currentPath ? currentPath.split('/') : [];

  let html = `<button class="breadcrumb-root" data-folder="">${escapeHtml(setName)}</button>`;
  let accumulated = '';
  for (const part of parts) {
    accumulated = accumulated ? `${accumulated}/${part}` : part;
    html += ` <span class="breadcrumb-sep">/</span> <button class="breadcrumb-part" data-folder="${escapeHtml(accumulated)}">${escapeHtml(part)}</button>`;
  }
  el.innerHTML = html;
  el.classList.remove('hidden');
  el.querySelectorAll('button').forEach((b) => {
    b.addEventListener('click', () => {
      state.folderPath = b.dataset.folder;
      loadMedia();
    });
  });
}

function enterFolder(name) {
  state.folderPath = state.folderPath ? `${state.folderPath}/${name}` : name;
  loadMedia();
}

function mediaWithBrowsePath(media, path) {
  return media.map((m) => ({ ...m, browse_path: path || '' }));
}

function navigateBack() {
  if (!state.folderPath) return;
  const last = state.folderPath.lastIndexOf('/');
  if (last < 0) {
    state.folderPath = '';
  } else {
    state.folderPath = state.folderPath.substring(0, last);
  }
  loadMedia();
}

function renderBrowse(data) {
  const grid = document.getElementById('media-grid');
  if (!grid) return;
  const folders = data.folders || [];
  const media = mediaWithBrowsePath(data.media || [], data.current_path || '');
  if (!folders.length && !media.length) {
    grid.innerHTML = '<p class="text-muted text-sm grid-full">Folder is empty.</p>';
    clearSelection();
    return;
  }
  const folderHtml = folders.map((f, i) => renderFolder(f, i)).join('');
  const mediaHtml = media.map((m, i) => renderItem(m, i)).join('');
  grid.innerHTML = folderHtml + mediaHtml;
  clearSelection();

  grid.querySelectorAll('.folder-card').forEach((el) => {
    el.addEventListener('click', () => enterFolder(el.dataset.name));
  });

  grid.querySelectorAll('.folder-card [data-action="regen-folder-cover"]').forEach((b) => {
    b.addEventListener('click', async (e) => {
      e.stopPropagation();
      const setId = state.selectedSetId;
      if (!setId) return;
      const name = b.closest('.folder-card')?.dataset.name;
      const folder = state.folderPath ? `${state.folderPath}/${name}` : name;
      try {
        await API.regenCover(setId, folder);
        toast('Folder cover regenerated');
        await loadMedia();
        // Force refresh the folder cover image to bypass browser cache
        const folderCard = document.querySelector(`.folder-card[data-name="${CSS.escape(name)}"]`);
        const img = folderCard?.querySelector('img');
        if (img) {
          const base = img.src.split('?')[0];
          img.src = `${base}?t=${Date.now()}`;
        }
      }
      catch (err) { toast(err.message || 'Cover failed', 'error'); }
    });
  });

  grid.querySelectorAll('.media-card, .media-row').forEach((el) => {
    el.addEventListener('click', () => { selectByElement(el); });
    const playBtn = el.querySelector('[data-action="play"]');
    const viewBtn = el.querySelector('[data-action="view"]');
    const favBtn = el.querySelector('[data-action="favorite"]');
    const noteBtn = el.querySelector('[data-action="notes"]');
    const downloadBtn = el.querySelector('[data-action="download"]');
    const tagBtn = el.querySelector('[data-action="tags"]');
    const thumbBtn = el.querySelector('[data-action="regen-thumb"]');
    playBtn?.addEventListener('click', (e) => { e.stopPropagation(); selectByElement(el); playSelected(); });
    viewBtn?.addEventListener('click', (e) => {
      e.stopPropagation();
      selectByElement(el);
      const idx = parseInt(el.dataset.index, 10);
      const media = state.media[idx];
      if (media) openLightbox(state.media, media.id);
    });
    favBtn?.addEventListener('click', (e) => { e.stopPropagation(); toggleFavorite(el.dataset.id, favBtn); });
    noteBtn?.addEventListener('click', (e) => { e.stopPropagation(); openNotesForSelected(); });
    downloadBtn?.addEventListener('click', (e) => { e.stopPropagation(); window.open(`/api/media/${el.dataset.id}/download`, '_blank'); });
    tagBtn?.addEventListener('click', (e) => { e.stopPropagation(); openTagsForElement(el); });
    thumbBtn?.addEventListener('click', (e) => { e.stopPropagation(); regenThumb(el.dataset.id); });
  });
}

function renderFolder(folder, index) {
  const name = typeof folder === 'string' ? folder : folder.name;
  const hasCover = typeof folder === 'object' && folder?.has_cover;
  const coverImg = hasCover
    ? `<img src="/api/sets/${state.selectedSetId}/cover?folder=${encodeURIComponent(state.folderPath ? `${state.folderPath}/${name}` : name)}" alt="" loading="lazy">`
    : null;
  return `
    <div class="media-card folder-card" data-name="${escapeHtml(name)}" data-index="${index}" tabindex="0" role="button" aria-label="Folder ${escapeHtml(name)}">
      <div class="thumb-wrap">
        ${hasCover ? coverImg : '<span class="placeholder">📁</span>'}
        <div class="card-actions">
          <button class="icon-btn btn-sm" data-action="regen-folder-cover" title="Regenerate cover">🔄</button>
        </div>
      </div>
      <div class="meta">
        <div class="title">${escapeHtml(name)}</div>
        <div class="subtitle">Folder</div>
      </div>
    </div>
  `;
}

function renderGrid(items) {
  const grid = document.getElementById('media-grid');
  if (!grid) return;
  if (!items.length) { grid.innerHTML = `<p class="text-muted text-sm grid-full">No results.</p>`; clearSelection(); return; }
  grid.innerHTML = items.map((m, i) => renderItem(m, i)).join('');
  clearSelection();
  grid.querySelectorAll('.media-card, .media-row').forEach((el) => {
    el.addEventListener('click', () => { selectByElement(el); });
    const playBtn = el.querySelector('[data-action="play"]');
    const viewBtn = el.querySelector('[data-action="view"]');
    const favBtn = el.querySelector('[data-action="favorite"]');
    const noteBtn = el.querySelector('[data-action="notes"]');
    const downloadBtn = el.querySelector('[data-action="download"]');
    const tagBtn = el.querySelector('[data-action="tags"]');
    const thumbBtn = el.querySelector('[data-action="regen-thumb"]');
    playBtn?.addEventListener('click', (e) => { e.stopPropagation(); selectByElement(el); playSelected(); });
    viewBtn?.addEventListener('click', (e) => {
      e.stopPropagation();
      selectByElement(el);
      const idx = parseInt(el.dataset.index, 10);
      const media = state.media[idx];
      if (media) openLightbox(state.media, media.id);
    });
    favBtn?.addEventListener('click', (e) => { e.stopPropagation(); toggleFavorite(el.dataset.id, favBtn); });
    noteBtn?.addEventListener('click', (e) => { e.stopPropagation(); openNotesForSelected(); });
    downloadBtn?.addEventListener('click', (e) => { e.stopPropagation(); window.open(`/api/media/${el.dataset.id}/download`, '_blank'); });
    tagBtn?.addEventListener('click', (e) => { e.stopPropagation(); openTagsForElement(el); });
    thumbBtn?.addEventListener('click', (e) => { e.stopPropagation(); regenThumb(el.dataset.id); });
  });
}

function renderItem(m, index) {
  const sizeText = fmtSize(m.file_size_bytes);
  if (m.type === 'video') {
    return `
      <div class="media-card" data-id="${m.id}" data-index="${index}" tabindex="0" role="button" aria-label="${escapeHtml(m.file_name)}">
        <div class="thumb-wrap">
          ${m.thumbnail_path ? `<img src="/api/media/${m.id}/thumbnail" alt="" loading="lazy">` : `<span class="placeholder">No image</span>`}
          <span class="badge">${fmtDur(m.duration)}${sizeText ? ' • ' + sizeText : ''}</span>
          <div class="card-actions">
            <button class="icon-btn btn-sm" data-action="play" title="Play">▶</button>
            <button class="icon-btn btn-sm" data-action="favorite" title="Favorite">♥</button>
            <button class="icon-btn btn-sm" data-action="notes" title="Notes">📝</button>
            <button class="icon-btn btn-sm" data-action="download" title="Download">⬇</button>
            <button class="icon-btn btn-sm" data-action="tags" title="Tags">🏷</button>
            <button class="icon-btn btn-sm" data-action="regen-thumb" title="Regenerate thumbnail">🔄</button>
          </div>
        </div>
        <div class="meta">
          <div class="title">${escapeHtml(m.file_name)}</div>
          <div class="subtitle">${escapeHtml(m.codec || '')} ${escapeHtml(m.resolution || '')} ${m.bitrate ? Math.round(m.bitrate / 1000) + 'kbps' : ''}</div>
        </div>
      </div>
    `;
  }
  if (m.type === 'image') {
    const resText = escapeHtml(m.resolution || '');
    const safeSizeText = escapeHtml(sizeText || '');
    return `
      <div class="media-card image-card" data-id="${m.id}" data-index="${index}" tabindex="0" role="button" aria-label="${escapeHtml(m.file_name)}">
        <div class="thumb-wrap">
          ${m.thumbnail_path ? `<img src="/api/media/${m.id}/thumbnail" alt="" loading="lazy">` : `<span class="placeholder">No image</span>`}
          <span class="badge">${resText}${safeSizeText ? ' • ' + safeSizeText : ''}</span>
          <div class="card-actions">
            <button class="icon-btn btn-sm" data-action="view" title="View">👁</button>
            <button class="icon-btn btn-sm" data-action="favorite" title="Favorite">♥</button>
            <button class="icon-btn btn-sm" data-action="notes" title="Notes">📝</button>
            <button class="icon-btn btn-sm" data-action="download" title="Download">⬇</button>
            <button class="icon-btn btn-sm" data-action="tags" title="Tags">🏷</button>
            <button class="icon-btn btn-sm" data-action="regen-thumb" title="Regenerate thumbnail">🔄</button>
          </div>
        </div>
        <div class="meta">
          <div class="title">${escapeHtml(m.file_name)}</div>
          <div class="subtitle">${resText} ${safeSizeText}</div>
        </div>
      </div>
    `;
  }
  return `
    <div class="media-card audio-card" data-id="${m.id}" data-index="${index}" tabindex="0" role="button" aria-label="${escapeHtml(m.file_name)}">
      <div class="thumb-wrap">
        ${m.thumbnail_path ? `<img src="/api/media/${m.id}/thumbnail" alt="" loading="lazy">` : `<span class="placeholder">No cover</span>`}
        <span class="badge">${fmtDur(m.duration)}${sizeText ? ' • ' + sizeText : ''}</span>
        <div class="card-actions">
          <button class="icon-btn btn-sm" data-action="play" title="Play">▶</button>
          <button class="icon-btn btn-sm" data-action="favorite" title="Favorite">♥</button>
          <button class="icon-btn btn-sm" data-action="notes" title="Notes">📝</button>
          <button class="icon-btn btn-sm" data-action="download" title="Download">⬇</button>
          <button class="icon-btn btn-sm" data-action="tags" title="Tags">🏷</button>
        </div>
      </div>
      <div class="meta">
        <div class="title">${escapeHtml(m.file_name)}</div>
        <div class="subtitle">${escapeHtml(m.codec || '')} ${m.bitrate ? Math.round(m.bitrate/1000)+'kbps' : ''}</div>
      </div>
    </div>
  `;
}

async function playSelected() {
  const el = currentElement();
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

async function navigatePlayable(delta, options = {}) {
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
  return !!state.selectedSetId && state.selectedSetIds.length === 1 && !isShuffle() && !hasActiveFilters();
}

function hasActiveFilters() {
  return !!(
    state.filters.search ||
    state.filters.type ||
    state.filters.favorites ||
    state.filters.tags ||
    state.filters.minDuration ||
    state.filters.maxDuration ||
    state.filters.minFileSize ||
    state.filters.maxFileSize ||
    state.filters.sort
  );
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

function seekByKeyboard(direction, repeated) {
  if (!hasLoadedMedia()) return false;
  if (!document.fullscreenElement) return false;
  const step = repeated ? 15 : 5;
  return seekRelative(direction * step);
}

function initMediaInfo() {
  const modal = document.getElementById('media-info-modal');
  const closeBtn = document.getElementById('media-info-close');
  closeBtn?.addEventListener('click', closeMediaInfo);
  modal?.addEventListener('click', (e) => { if (e.target === modal) closeMediaInfo(); });
}

function closeMediaInfo() {
  document.getElementById('media-info-modal')?.classList.remove('open');
}

function isMediaInfoOpen() {
  return document.getElementById('media-info-modal')?.classList.contains('open');
}

function scrollMediaInfo(delta) {
  const panel = document.getElementById('media-info-panel')
    || document.querySelector('#media-info-modal .modal');
  if (!panel) return;
  const step = Math.max(96, Math.round(panel.clientHeight * 0.25));
  panel.scrollTop += delta * step;
}

async function toggleMediaInfo() {
  const modal = document.getElementById('media-info-modal');
  if (!modal) return;
  if (modal.classList.contains('open')) {
    closeMediaInfo();
    return;
  }
  const id = selectedMediaId();
  if (!id) {
    toast('No media selected', 'error');
    return;
  }
  await openMediaInfo(id);
}

function selectedMediaId() {
  const el = currentElement();
  if (el?.dataset?.id) return el.dataset.id;
  const id = currentMediaId();
  return id ? String(id) : '';
}

async function openMediaInfo(id) {
  const modal = document.getElementById('media-info-modal');
  const body = document.getElementById('media-info-body');
  if (!modal || !body) return;
  body.innerHTML = '<p class="text-muted text-sm">Loading...</p>';
  modal.classList.add('open');
  (document.getElementById('media-info-panel')
    || document.querySelector('#media-info-modal .modal'))?.focus({ preventScroll: true });
  try {
    const detail = await API.mediaDetail(id);
    body.innerHTML = renderMediaInfo(detail);
  } catch (err) {
    body.innerHTML = `<p class="error-message">${escapeHtml(err.message || 'Failed to load media info')}</p>`;
  }
}

function renderMediaInfo(detail) {
  const media = detail?.media || {};
  const tags = Array.isArray(detail?.tags) ? detail.tags.map((t) => t.name).filter(Boolean).join(', ') : '';
  const ext = media.file_name?.includes('.') ? media.file_name.split('.').pop().toUpperCase() : '';
  const progress = detail?.progress;
  const note = detail?.note;
  let rows = [
    ['Title', media.file_name],
    ['Format', ext],
    ['Type', media.type],
    ['File size', media.file_size_bytes ? `${fmtSize(media.file_size_bytes)} (${media.file_size_bytes} bytes)` : ''],
    ['Relative path', media.rel_path],
    ['Absolute path', media.abs_path],
    ['Media ID', media.id],
    ['Set ID', media.set_id],
    ['Play count', media.play_count],
    ['Added', fmtDateTime(media.created_at)],
    ['Thumbnail', media.thumbnail_path],
    ['Favorite', detail?.favorite ? 'Yes' : 'No'],
    ['Tags', tags],
  ];
  if (media.type === 'image') {
    rows = rows.concat([
      ['Dimensions', media.resolution],
      ['Width', media.width],
      ['Height', media.height],
      ['Camera', media.exif_camera],
      ['Lens', media.exif_lens],
      ['Date Taken', media.exif_date],
      ['ISO', media.exif_iso],
      ['F-Number', media.exif_f_number],
      ['Exposure', media.exif_exposure],
      ['Focal Length', media.exif_focal_length],
    ]);
  } else {
    rows = rows.concat([
      ['Duration', media.duration ? `${fmtDur(media.duration)} (${Math.round(media.duration)} seconds)` : ''],
      ['Bitrate', media.bitrate ? `${Math.round(media.bitrate / 1000)} kbps (${media.bitrate} bps)` : ''],
      ['Codec', media.codec],
      ['Resolution', media.resolution],
      ['Saved position', progress ? `${fmtDur(progress.position_seconds)} (${Math.round(progress.position_seconds || 0)} seconds)` : ''],
      ['Progress updated', fmtDateTime(progress?.updated_at)],
    ]);
  }
  rows = rows.concat([
    ['Note updated', fmtDateTime(note?.updated_at)],
    ['Note length', note?.content ? `${note.content.length} characters` : ''],
  ]);
  const table = rows
    .filter(([, value]) => value !== undefined && value !== null && value !== '')
    .map(([label, value]) => `<tr><th scope="row">${escapeHtml(label)}</th><td>${escapeHtml(String(value))}</td></tr>`)
    .join('');
  const raw = escapeHtml(JSON.stringify(detail || {}, null, 2));
  return `
    <table class="media-info-table">${table}</table>
    <details class="media-info-raw">
      <summary>Raw API detail</summary>
      <pre>${raw}</pre>
    </details>
  `;
}

async function shareSelected() {
  const el = currentElement();
  if (!el) return;
  const id = el.dataset.id;
  try {
    const res = await API.share(id);
    const token = res?.token || res?.share?.token;
    const url = `${location.origin}/s/${token}`;
    navigator.clipboard?.writeText(url);
    toast('Share link copied');
  } catch (err) { toast(err.message || 'Share failed', 'error'); }
}

async function toggleFavorite(id, btn) {
  try {
    await API.favorite(id);
    btn?.classList.toggle('active');
  } catch (err) { toast(err.message || 'Favorite failed', 'error'); }
}

async function openNotesForSelected() {
  const el = currentElement();
  if (!el) return;
  const id = el.dataset.id;
  let content = '';
  try {
    const note = await API.notes(id);
    content = note?.content || '';
  } catch {}
  openNotes(id, content);
}

async function downloadSelected() {
  const el = currentElement();
  if (!el) return;
  window.open(`/api/media/${el.dataset.id}/download`, '_blank');
}

// Tags modal
let tagsCurrentMediaId = null;

async function openTagsForSelected() {
  const el = currentElement();
  if (!el) return;
  openTagsForElement(el);
}

async function openTagsForElement(el) {
  const id = el.dataset.id;
  tagsCurrentMediaId = id;
  const detail = await API.mediaDetail(id);
  const tags = detail?.tags || [];
  renderTagsList(tags);
  document.getElementById('tags-modal')?.classList.add('open');
  document.getElementById('tags-new')?.focus();
}

function closeTagsModal() {
  document.getElementById('tags-modal')?.classList.remove('open');
  tagsCurrentMediaId = null;
}

function renderTagsList(tags) {
  const el = document.getElementById('tags-list');
  if (!el) return;
  if (!tags.length) { el.innerHTML = '<span class="text-muted text-xs">No tags.</span>'; return; }
  el.innerHTML = tags.map((t) =>
    `<span class="tag-chip">${escapeHtml(t.name)} <button class="icon-btn btn-sm tag-remove" data-tag="${escapeHtml(t.name)}" title="Remove">✕</button></span>`
  ).join('');
  el.querySelectorAll('.tag-remove').forEach((b) => {
    b.addEventListener('click', async () => {
      if (!tagsCurrentMediaId) return;
      try {
        await API.removeTag(tagsCurrentMediaId, b.dataset.tag);
        const detail = await API.mediaDetail(tagsCurrentMediaId);
        renderTagsList(detail?.tags || []);
      } catch (err) { toast(err.message || 'Remove tag failed', 'error'); }
    });
  });
}

async function addTagForSelected() {
  if (!tagsCurrentMediaId) return;
  const input = document.getElementById('tags-new');
  const name = input?.value.trim();
  if (!name) return;
  try {
    await API.addTag(tagsCurrentMediaId, name);
    input.value = '';
    const detail = await API.mediaDetail(tagsCurrentMediaId);
    renderTagsList(detail?.tags || []);
  } catch (err) { toast(err.message || 'Add tag failed', 'error'); }
}

async function regenThumb(mediaId) {
  try {
    await API.regenThumbnail(mediaId);
    toast('Thumbnail regenerated');
    // Force refresh any thumbnail images for this media in the grid
    document.querySelectorAll(`#media-grid [data-id="${mediaId}"] img`).forEach((img) => {
      const base = img.src.split('?')[0];
      img.src = `${base}?t=${Date.now()}`;
    });
    // If the same media is currently loaded in the player, refresh cover art too
    const e = document.getElementById('cover-art');
    if (e && !e.classList.contains('hidden')) {
      const base = e.src.split('?')[0];
      e.src = `${base}?t=${Date.now()}`;
    }
  } catch (err) { toast(err.message || 'Thumbnail failed', 'error'); }
}

// Upload modal
function initUpload() {
  const modal = document.getElementById('upload-modal');
  const closeBtn = document.getElementById('upload-close');
  const form = document.getElementById('upload-form');
  const fileInput = document.getElementById('upload-file');

  closeBtn?.addEventListener('click', () => modal?.classList.remove('open'));
  modal?.addEventListener('click', (e) => { if (e.target === modal) modal.classList.remove('open'); });

  form?.addEventListener('submit', async (e) => {
    e.preventDefault();
    if (!state.selectedSetId) { toast('Select a set first', 'error'); return; }
    const file = fileInput?.files[0];
    if (!file) { toast('Choose a file', 'error'); return; }
    const fd = new FormData();
    fd.append('file', file);
    try {
      await API.upload(state.selectedSetId, fd);
      toast('Upload complete');
      fileInput.value = '';
      modal?.classList.remove('open');
      loadMedia();
    } catch (err) { toast(err.message || 'Upload failed', 'error'); }
  });
}

function showUpload() {
  const modal = document.getElementById('upload-modal');
  modal?.classList.add('open');
}

// Help modal
function initHelp() {
  const modal = document.getElementById('help-modal');
  const closeBtn = document.getElementById('help-close');
  const toggleBtn = document.getElementById('help-toggle');

  toggleBtn?.addEventListener('click', toggleHelp);
  closeBtn?.addEventListener('click', () => modal?.classList.remove('open'));
  modal?.addEventListener('click', (e) => { if (e.target === modal) modal.classList.remove('open'); });
}

function toggleSidebar() {
  const sidebar = document.getElementById('sidebar');
  const page = document.querySelector('.page');
  if (!sidebar) return;
  sidebar.classList.toggle('open');
  page?.classList.toggle('has-sidebar', sidebar.classList.contains('open'));
}

function toggleHelp() {
  const modal = document.getElementById('help-modal');
  if (!modal) return;
  modal.classList.toggle('open');
}

function showSearch() {
  const bar = document.getElementById('search-bar');
  bar?.classList.remove('hidden');
  focusSearch();
}

function closeAllModals() {
  document.querySelectorAll('.modal-overlay.open').forEach((m) => m.classList.remove('open'));
}

function showAdmin() {
  const btn = document.getElementById('admin-toggle');
  if (btn) btn.classList.remove('hidden');
  startScanProgressPolling();
}

function startScanProgressPolling() {
  if (scanProgressTimer) return;
  pollScanProgress();
  scanProgressTimer = setInterval(pollScanProgress, 2000);
}

async function pollScanProgress() {
  try {
    renderScanProgress(await API.scanProgress());
  } catch {}
}

function renderScanProgress(progress) {
  const indicator = document.getElementById('scan-indicator');
  const text = document.getElementById('scan-indicator-text');
  if (!indicator || !text || !progress) return;

  if (!progress.running) {
    indicator.classList.add('hidden');
    return;
  }

  const setPart = progress.current_set ? ` ${progress.current_set}` : '';
  const setsTotal = progress.sets_total || 0;
  const setsPart = setsTotal ? ` ${progress.sets_done || 0}/${setsTotal} sets` : '';
  const filesPart = progress.files_done ? `, ${progress.files_done} files` : '';
  text.textContent = `Scanning${setPart}${setsPart}${filesPart}`;
  indicator.classList.remove('hidden');
}

// Shares modal
function initShares() {
  const modal = document.getElementById('shares-modal');
  const closeBtn = document.getElementById('shares-close');

  closeBtn?.addEventListener('click', () => closeSharesModal());
  modal?.addEventListener('click', (e) => { if (e.target === modal) closeSharesModal(); });
}

function closeSharesModal() {
  const modal = document.getElementById('shares-modal');
  modal?.classList.remove('open');
  state.sharesCurrentRow = -1;
}

async function toggleShares() {
  const modal = document.getElementById('shares-modal');
  if (!modal) return;
  if (modal.classList.contains('open')) {
    closeSharesModal();
    return;
  }
  try {
    const shares = await API.myShares();
    renderSharesList(shares || []);
    modal.classList.add('open');
  } catch (err) {
    toast(err.message || 'Failed to load shares', 'error');
  }
}

function renderSharesList(shares) {
  const el = document.getElementById('shares-list');
  if (!el) return;
  state.sharesData = shares;
  state.sharesCurrentRow = -1;
  if (!shares.length) {
    el.innerHTML = '<p class="text-muted text-sm">No share links yet.</p>';
    return;
  }
  const now = new Date();
  el.innerHTML = shares.map((sh, i) => {
    const expires = new Date(sh.expires_at);
    const expired = expires < now;
    const url = `${location.origin}/s/${sh.token}`;
    return `
      <div class="share-row flex gap-2 align-center py-1 border-b${i === 0 ? ' selected' : ''}" data-index="${i}" tabindex="0">
        <span class="flex-1 text-sm truncate">${escapeHtml(sh.file_name || 'Unknown')}</span>
        <span class="text-xs text-muted">${sh.media_type === 'video' ? '🎬' : sh.media_type === 'image' ? '🖼️' : '🎵'} ${expired ? '<span class="text-danger">Expired</span>' : fmtDate(expires)}</span>
        <button class="icon-btn btn-sm" title="Copy link" data-copy="${url}">&#128203;</button>
        <button class="icon-btn btn-sm" title="Revoke" data-revoke="${sh.token}">&#10005;</button>
      </div>
    `;
  }).join('');
  state.sharesCurrentRow = 0;
  updateSharesSelection();
  el.querySelectorAll('[data-copy]').forEach((b) => {
    b.addEventListener('click', () => {
      navigator.clipboard?.writeText(b.dataset.copy);
      toast('Link copied');
    });
  });
  el.querySelectorAll('[data-revoke]').forEach((b) => {
    b.addEventListener('click', async () => {
      try {
        await API.revokeShare(b.dataset.revoke);
        toast('Share revoked');
        const shares = await API.myShares();
        renderSharesList(shares || []);
      } catch (err) { toast(err.message || 'Revoke failed', 'error'); }
    });
  });
}

function updateSharesSelection() {
  const rows = document.querySelectorAll('#shares-list .share-row');
  rows.forEach((r, i) => {
    r.classList.toggle('selected', i === state.sharesCurrentRow);
    if (i === state.sharesCurrentRow) r.focus();
  });
}

function sharesNav(delta) {
  const rows = document.querySelectorAll('#shares-list .share-row');
  if (!rows.length) return;
  state.sharesCurrentRow = Math.max(0, Math.min(rows.length - 1, state.sharesCurrentRow + delta));
  updateSharesSelection();
}

function copySelectedShare() {
  const rows = document.querySelectorAll('#shares-list .share-row');
  const row = rows[state.sharesCurrentRow];
  if (!row) return;
  const copyBtn = row.querySelector('[data-copy]');
  if (!copyBtn) { toast('Nothing to copy'); return; }
  navigator.clipboard?.writeText(copyBtn.dataset.copy).then(() => toast('Share link copied'));
}

async function deleteSelectedShare() {
  const rows = document.querySelectorAll('#shares-list .share-row');
  const row = rows[state.sharesCurrentRow];
  if (!row) { toast('No share selected'); return; }
  const revokeBtn = row.querySelector('[data-revoke]');
  if (!revokeBtn) return;
  try {
    await API.revokeShare(revokeBtn.dataset.revoke);
    toast('Share revoked');
    const shares = await API.myShares();
    renderSharesList(shares || []);
  } catch (err) { toast(err.message || 'Revoke failed', 'error'); }
}

function fmtDate(d) {
  if (!d) return '';
  const dt = typeof d === 'string' ? new Date(d) : d;
  return dt.toLocaleDateString();
}

function fmtDateTime(d) {
  if (!d) return '';
  const dt = typeof d === 'string' ? new Date(d) : d;
  if (Number.isNaN(dt.getTime())) return '';
  return dt.toLocaleString();
}

function fmtDur(s) {
  if (!s || s < 0) return '0:00';
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = Math.floor(s % 60);
  const mm = String(m).padStart(2, '0');
  const ss = String(sec).padStart(2, '0');
  return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`;
}

function fmtSize(bytes) {
  if (!bytes || bytes <= 0) return '';
  const kb = bytes / 1024;
  if (kb < 1024) return Math.round(kb) + ' KB';
  const mb = kb / 1024;
  if (mb < 1024) return Math.round(mb * 10) / 10 + ' MB';
  return Math.round((mb / 1024) * 10) / 10 + ' GB';
}

function escapeHtml(s) {
  return (s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function toast(msg, type = 'info') {
  const t = document.getElementById('toast');
  if (!t) return;
  t.textContent = msg;
  t.className = 'toast show ' + type;
  setTimeout(() => t.classList.remove('show'), 2800);
}
