import { API } from './api.js';
import { initKeyboard } from './keyboard.js';
import { initSelection, select, selectByElement, next, prev, currentIndex, currentElement } from './selection.js';
import { initPlayer, togglePlay, toggleFullscreen, exitFullscreenIfNeeded, currentMediaId, selectAndPlay, playPrevious, playNext } from './player.js';
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
      loadMedia();
    },
    input: document.getElementById('search-input'),
    clearBtn: document.getElementById('search-clear'),
  });
  initShuffle({
    onChange: () => loadMedia(),
  });
  initKeyboard({
    navUp: () => prev(),
    navDown: () => next(),
    navLeft: () => setSetByDelta(-1),
    navRight: () => setSetByDelta(1),
    enter: () => {
      const el = currentElement();
      if (el) { selectByElement(el); playSelected(); }
    },
    playPause: () => togglePlay(),
    fullscreen: () => toggleFullscreen(),
    escape: () => { exitFullscreenIfNeeded(); const el = currentElement(); if (el) el.classList.remove('selected'); },
    shuffle: () => { toggleShuffle(); loadMedia(); },
    share: () => shareSelected(),
    search: focusSearch,
    notes: openNotesForSelected,
    tags: openTagsForSelected,
    download: downloadSelected,
  });
  initNotes(() => toast('Note saved'));
  initAdmin();
  initPWA();
  initUpload();

  // Filters
  document.getElementById('filter-type')?.addEventListener('change', (e) => { state.filters.type = e.target.value; loadMedia(); });
  document.getElementById('filter-favorites')?.addEventListener('click', (e) => {
    state.filters.favorites = !state.filters.favorites;
    e.target.classList.toggle('active', state.filters.favorites);
    loadMedia();
  });
  document.getElementById('filter-tags')?.addEventListener('change', (e) => { state.filters.tags = e.target.value; loadMedia(); });
  document.getElementById('filter-min-duration')?.addEventListener('change', (e) => { state.filters.minDuration = e.target.value; loadMedia(); });
  document.getElementById('filter-max-duration')?.addEventListener('change', (e) => { state.filters.maxDuration = e.target.value; loadMedia(); });
  document.getElementById('filter-min-filesize')?.addEventListener('change', (e) => { state.filters.minFilesizeMB = e.target.value; loadMedia(); });
  document.getElementById('filter-max-filesize')?.addEventListener('change', (e) => { state.filters.maxFilesizeMB = e.target.value; loadMedia(); });

  // Menu toggle
  document.getElementById('menu-btn')?.addEventListener('click', () => {
    document.getElementById('sidebar')?.classList.toggle('open');
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
    if (state.sets.length) { state.selectedSetId = state.sets[0].id; setActiveSet(state.sets[0].id); loadMedia(); }
  } catch (err) {
    toast(err.message || 'Error loading sets', 'error');
  }

  // Play on double-click
  document.getElementById('media-grid')?.addEventListener('dblclick', (e) => {
    const el = e.target.closest('.media-card, .media-row');
    if (el) { selectByElement(el); playSelected(); }
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
    `<div class="set-row">
      <a href="#" class="set-item flex-1-18" data-id="${s.id}">${escapeHtml(s.name)}</a>
      <button class="icon-btn btn-sm" data-cover-set="${s.id}" title="Regenerate cover">🔄</button>
    </div>`
  ).join('');
  el.querySelectorAll('.set-item').forEach((a) => {
    a.addEventListener('click', (e) => {
      e.preventDefault();
      state.selectedSetId = parseInt(a.dataset.id, 10);
      setActiveSet(state.selectedSetId);
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
  setActiveSet(state.selectedSetId);
  loadMedia();
}

async function loadMedia() {
  const grid = document.getElementById('media-grid');
  if (!grid) return;
  grid.innerHTML = '<p class="text-muted text-sm grid-full">Loading...</p>';
  try {
    const params = {
      set_id: state.selectedSetId ?? '',
      type: state.filters.type,
      search: state.filters.search,
      favorites: state.filters.favorites ? 'true' : '',
      tags: state.filters.tags || '',
      min_duration: state.filters.minDuration || '',
      max_duration: state.filters.maxDuration || '',
      filesize_min: state.filters.minFilesizeMB ? String(parseInt(state.filters.minFilesizeMB, 10) << 20) : '',
      filesize_max: state.filters.maxFilesizeMB ? String(parseInt(state.filters.maxFilesizeMB, 10) << 20) : '',
      sort: isShuffle() ? 'random' : 'name',
      limit: '200',
    };
    const data = await API.media(params);
    const list = Array.isArray(data) ? data : data?.media || [];
    setMedia(list);
    renderGrid(list);
    document.getElementById('result-count').textContent = `${list.length} items`;
  } catch (err) {
    grid.innerHTML = `<p class="error-message grid-full">${escapeHtml(err.message)}</p>`;
  }
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
        </div>
        <div class="meta">
          <div class="title">${escapeHtml(m.file_name)}</div>
          <div class="subtitle">${escapeHtml(m.codec || '')} ${m.resolution || ''} ${m.bitrate ? Math.round(m.bitrate / 1000) + 'kbps' : ''}</div>
        </div>
        <div class="card-actions">
          <button class="icon-btn btn-sm" data-action="play" title="Play">▶</button>
          <button class="icon-btn btn-sm" data-action="favorite" title="Favorite">♥</button>
          <button class="icon-btn btn-sm" data-action="notes" title="Notes">📝</button>
          <button class="icon-btn btn-sm" data-action="download" title="Download">⬇</button>
          <button class="icon-btn btn-sm" data-action="tags" title="Tags">🏷</button>
          <button class="icon-btn btn-sm" data-action="regen-thumb" title="Regenerate thumbnail">🔄</button>
        </div>
      </div>
    `;
  }
  return `
    <div class="media-row" data-id="${m.id}" data-index="${index}" tabindex="0" role="button" aria-label="${escapeHtml(m.file_name)}">
      <span class="row-icon">🎵</span>
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
  } catch (err) { toast(err.message || 'Thumbnail failed', 'error'); }
}

// Upload modal
function initUpload() {
  const modal = document.getElementById('upload-modal');
  const closeBtn = document.getElementById('upload-close');
  const form = document.getElementById('upload-form');
  const fileInput = document.getElementById('upload-file');

  // Open via an icon button in header (already added in HTML)
  document.getElementById('upload-toggle')?.addEventListener('click', () => modal?.classList.add('open'));
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
