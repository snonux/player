import { API } from './api.js';
import { initKeyboard } from './keyboard.js';
import { initSelection, select, selectByElement, next, prev, currentIndex, currentElement, navUp, navDown, navLeft, navRight } from './selection.js';
import { initPlayer, togglePlay, toggleFullscreen, toggleStage, exitFullscreenIfNeeded, currentMediaId, selectAndPlay, playPrevious, playNext } from './player.js';
import { initSearch, focusSearch, trigger as triggerSearch } from './search.js';
import { initShuffle, toggle as toggleShuffle, isOn as isShuffle } from './shuffle.js';
import { initThemes } from './themes.js';
import { initNotes, open as openNotes } from './notes.js';
import { initAdmin } from './admin.js';
import { state, setMedia } from './state.js';
import { initPWA } from './pwa.js';

const pageMap = { '/index.html': 'spa', '/login.html': 'login', '/bootstrap.html': 'bootstrap' };

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
  initPlayer();
  initSelection();
  initSearch({
    onChange: (q) => {
      state.filters.search = q;
      state.folderPath = '';
      loadMedia();
    },
    input: document.getElementById('search-input'),
    clearBtn: document.getElementById('search-clear'),
  });
  initShuffle({
    onChange: () => loadMedia(),
  });
  initKeyboard({
    navUp: () => navUp(),
    navDown: () => navDown(),
    navLeft: () => navLeft(),
    navRight: () => navRight(),
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
        playSelected();
      }
    },
    playPause: () => togglePlay(),
    fullscreen: () => toggleFullscreen(),
    toggleStage: () => toggleStage(),
    escape: () => { exitFullscreenIfNeeded(); const el = currentElement(); if (el) el.classList.remove('selected'); closeAllModals(); },
    shuffle: () => { toggleShuffle(); loadMedia(); },
    share: () => shareSelected(),
    search: () => showSearch(),
    notes: openNotesForSelected,
    tags: openTagsForSelected,
    download: downloadSelected,
    help: toggleHelp,
    toolbar: toggleToolbar,
    backspace: () => { navigateBack(); },
    sidebar: toggleSidebar,
    upload: () => showUpload(),
    focusMinDuration: () => focusFilter('filter-min-duration'),
    focusMaxDuration: () => focusFilter('filter-max-duration'),
  });
  initNotes(() => toast('Note saved'));
  initAdmin();
  initPWA();
  initUpload();
  initHelp();

  // Filters
  document.getElementById('filter-type')?.addEventListener('change', (e) => { state.filters.type = e.target.value; state.folderPath = ''; loadMedia(); });
  document.getElementById('filter-favorites')?.addEventListener('click', (e) => {
    state.filters.favorites = !state.filters.favorites;
    e.target.classList.toggle('active', state.filters.favorites);
    state.folderPath = '';
    loadMedia();
  });
  document.getElementById('filter-tags')?.addEventListener('change', (e) => { state.filters.tags = e.target.value; state.folderPath = ''; loadMedia(); });
  document.getElementById('filter-min-duration')?.addEventListener('change', (e) => { state.filters.minDuration = e.target.value; state.folderPath = ''; loadMedia(); });
  document.getElementById('filter-max-duration')?.addEventListener('change', (e) => { state.filters.maxDuration = e.target.value; state.folderPath = ''; loadMedia(); });
  document.getElementById('filter-toggle')?.addEventListener('click', () => {
    document.getElementById('filter-advanced')?.classList.toggle('hidden');
  });

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

  // Play on double-click
  document.getElementById('media-grid')?.addEventListener('dblclick', (e) => {
    const el = e.target.closest('.media-card, .media-row');
    if (el) { selectByElement(el); playSelected(); }
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
    if (singleSetId && !setIds && !state.filters.search && !state.filters.type && !state.filters.favorites && !state.filters.tags && !state.filters.minDuration && !state.filters.maxDuration) {
      const data = await API.browse(singleSetId, state.folderPath);
      updateBreadcrumb(data.current_path);
      setMedia(data.media || []);
      renderBrowse(data);
      const total = (data.media?.length || 0) + (data.folders?.length || 0);
      resultCount.textContent = `${total} items`;
    } else {
      breadcrumb?.classList.add('hidden');
      const params = {
        set_id: setIds ? '' : String(singleSetId || state.selectedSetId || ''),
        set_ids: setIds,
        type: state.filters.type,
        search: state.filters.search,
        favorites: state.filters.favorites ? 'true' : '',
        tags: state.filters.tags || '',
        min_duration: state.filters.minDuration ? String(parseFloat(state.filters.minDuration) * 60) : '',
        max_duration: state.filters.maxDuration ? String(parseFloat(state.filters.maxDuration) * 60) : '',
        sort: isShuffle() ? 'random' : 'name',
        limit: '200',
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
    html += ` <span class="breadcrumb-sep">/</span> <button class="breadcrumb-part" data-folder="${accumulated}">${escapeHtml(part)}</button>`;
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
  const media = data.media || [];
  if (!folders.length && !media.length) {
    grid.innerHTML = '<p class="text-muted text-sm grid-full">Folder is empty.</p>';
    return;
  }
  const folderHtml = folders.map((f, i) => renderFolder(f, i)).join('');
  const mediaHtml = media.map((m, i) => renderItem(m, i + folders.length)).join('');
  grid.innerHTML = folderHtml + mediaHtml;

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
      try { await API.regenCover(setId, folder); toast('Folder cover regenerated'); loadMedia(); }
      catch (err) { toast(err.message || 'Cover failed', 'error'); }
    });
  });

  grid.querySelectorAll('.media-card, .media-row').forEach((el) => {
    el.addEventListener('click', () => { selectByElement(el); });
    const playBtn = el.querySelector('[data-action="play"]');
    const favBtn = el.querySelector('[data-action="favorite"]');
    const noteBtn = el.querySelector('[data-action="notes"]');
    const downloadBtn = el.querySelector('[data-action="download"]');
    const tagBtn = el.querySelector('[data-action="tags"]');
    const thumbBtn = el.querySelector('[data-action="regen-thumb"]');
    playBtn?.addEventListener('click', (e) => { e.stopPropagation(); selectByElement(el); playSelected(); });
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
  if (!items.length) { grid.innerHTML = `<p class="text-muted text-sm grid-full">No results.</p>`; return; }
  grid.innerHTML = items.map((m, i) => renderItem(m, i)).join('');
  grid.querySelectorAll('.media-card, .media-row').forEach((el) => {
    el.addEventListener('click', () => { selectByElement(el); });
    const playBtn = el.querySelector('[data-action="play"]');
    const favBtn = el.querySelector('[data-action="favorite"]');
    const noteBtn = el.querySelector('[data-action="notes"]');
    const downloadBtn = el.querySelector('[data-action="download"]');
    const tagBtn = el.querySelector('[data-action="tags"]');
    const thumbBtn = el.querySelector('[data-action="regen-thumb"]');
    playBtn?.addEventListener('click', (e) => { e.stopPropagation(); selectByElement(el); playSelected(); });
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
          <div class="subtitle">${escapeHtml(m.codec || '')} ${m.resolution || ''} ${m.bitrate ? Math.round(m.bitrate / 1000) + 'kbps' : ''}</div>
        </div>
      </div>
    `;
  }
  const coverHtml = m.thumbnail_path
    ? `<img src="/api/media/${m.id}/thumbnail" alt="" loading="lazy" class="row-cover">`
    : '<span class="row-icon">🎵</span>';
  return `
    <div class="media-row" data-id="${m.id}" data-index="${index}" tabindex="0" role="button" aria-label="${escapeHtml(m.file_name)}">
      ${coverHtml}
      <div class="row-body">
        <div class="row-title">${escapeHtml(m.file_name)}</div>
        <div class="row-meta">${escapeHtml(m.codec || '')} ${m.bitrate ? Math.round(m.bitrate/1000)+'kbps' : ''} ${sizeText ? '• ' + sizeText : ''}</div>
      </div>
      <span class="row-duration">${fmtDur(m.duration)}</span>
      <button class="icon-btn btn-sm" data-action="play" title="Play">▶</button>
      <button class="icon-btn btn-sm" data-action="favorite" title="Favorite">♥</button>
      <button class="icon-btn btn-sm" data-action="notes" title="Notes">📝</button>
      <button class="icon-btn btn-sm" data-action="download" title="Download">⬇</button>
      <button class="icon-btn btn-sm" data-action="tags" title="Tags">🏷</button>
      <button class="icon-btn btn-sm" data-action="regen-thumb" title="Regenerate thumbnail">🔄</button>
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

function toggleHelp() {
  const modal = document.getElementById('help-modal');
  if (!modal) return;
  modal.classList.toggle('open');
}

// Toolbar / sidebar / search visibility toggles
function toggleToolbar() {
  const toolbar = document.getElementById('toolbar');
  toolbar?.classList.toggle('hidden');
}

function toggleSidebar() {
  const sidebar = document.getElementById('sidebar');
  const page = document.querySelector('.page');
  if (!sidebar) return;
  const open = sidebar.classList.toggle('open');
  page?.classList.toggle('has-sidebar', open);
}

function focusFilter(id) {
  const el = document.getElementById(id);
  if (!el) return;
  document.getElementById('filter-advanced')?.classList.remove('hidden');
  el.focus();
  el.select();
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
