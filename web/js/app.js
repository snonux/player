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

const qs = document.querySelector.bind(document);
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
  });
  initNotes(() => toast('Note saved'));
  initAdmin();
  initPWA();

  // Filters
  document.getElementById('filter-type')?.addEventListener('change', (e) => { state.filters.type = e.target.value; loadMedia(); });
  document.getElementById('filter-favorites')?.addEventListener('click', (e) => {
    state.filters.favorites = !state.filters.favorites;
    e.target.classList.toggle('active', state.filters.favorites);
    loadMedia();
  });

  // Menu toggle
  document.getElementById('menu-btn')?.addEventListener('click', () => {
    document.getElementById('sidebar')?.classList.toggle('open');
  });
  document.getElementById('logout-btn')?.addEventListener('click', async () => {
    try { await API.logout(); location.href = '/login.html'; } catch {}
  });

  const user = await API.loginProfile ? API.loginProfile : undefined; // not a route; we just try to bootstrap auth via /api/sets
  try {
    const sets = await API.sets();
    state.sets = sets || [];
    state.isAdmin = !!state.sets?.__isAdmin; // backend may not send this; we'll infer from admin endpoint later
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
}

function renderSets() {
  const el = document.getElementById('set-list');
  if (!el) return;
  el.innerHTML = state.sets.map((s) =>
    `<a href="#" class="set-item" data-id="${s.id}">${escapeHtml(s.name)}</a>`
  ).join('');
  el.querySelectorAll('.set-item').forEach((a) => {
    a.addEventListener('click', (e) => {
      e.preventDefault();
      state.selectedSetId = parseInt(a.dataset.id, 10);
      setActiveSet(state.selectedSetId);
      loadMedia();
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
  grid.innerHTML = '<p class="text-muted" style="grid-column:1/-1;font-size:0.85rem;">Loading...</p>';
  try {
    const params = {
      set_id: state.selectedSetId ?? '',
      type: state.filters.type,
      search: state.filters.search,
      favorites: state.filters.favorites ? 'true' : '',
      sort: isShuffle() ? 'random' : 'name',
      limit: '200',
    };
    const data = await API.media(params);
    const list = Array.isArray(data) ? data : data?.media || [];
    setMedia(list);
    renderGrid(list);
    document.getElementById('result-count').textContent = `${list.length} items`;
  } catch (err) {
    grid.innerHTML = `<p class="error-message" style="grid-column:1/-1;">${escapeHtml(err.message)}</p>`;
  }
}

function renderGrid(items) {
  const grid = document.getElementById('media-grid');
  if (!grid) return;
  if (!items.length) { grid.innerHTML = `<p class="text-muted" style="grid-column:1/-1;font-size:0.85rem;">No results.</p>`; return; }
  grid.innerHTML = items.map((m, i) => renderItem(m, i)).join('');
  grid.querySelectorAll('.media-card, .media-row').forEach((el) => {
    el.addEventListener('click', () => { selectByElement(el); });
    const playBtn = el.querySelector('[data-action="play"]');
    const favBtn = el.querySelector('[data-action="favorite"]');
    const noteBtn = el.querySelector('[data-action="notes"]');
    playBtn?.addEventListener('click', (e) => { e.stopPropagation(); selectByElement(el); playSelected(); });
    favBtn?.addEventListener('click', (e) => { e.stopPropagation(); toggleFavorite(el.dataset.id, favBtn); });
    noteBtn?.addEventListener('click', (e) => { e.stopPropagation(); openNotesForSelected(); });
  });
}

function renderItem(m, index) {
  if (m.type === 'video') {
    return `
      <div class="media-card" data-id="${m.id}" data-index="${index}" tabindex="0" role="button" aria-label="${escapeHtml(m.file_name)}">
        <div class="thumb-wrap">
          ${m.thumbnail_path ? `<img src="/api/media/${m.id}/thumbnail" alt="" loading="lazy">` : `<span class="placeholder">No image</span>`}
          <span class="badge">${fmtDur(m.duration)}</span>
        </div>
        <div class="meta">
          <div class="title">${escapeHtml(m.file_name)}</div>
          <div class="subtitle">${escapeHtml(m.codec || '')} ${m.resolution || ''} ${m.bitrate ? Math.round(m.bitrate / 1000) + 'kbps' : ''}</div>
        </div>
        <div style="display:flex;gap:0.25rem;padding:0 0.5rem 0.5rem;">
          <button class="icon-btn btn-sm" data-action="play" title="Play">▶</button>
          <button class="icon-btn btn-sm" data-action="favorite" title="Favorite">♥</button>
          <button class="icon-btn btn-sm" data-action="notes" title="Notes">📝</button>
        </div>
      </div>
    `;
  }
  return `
    <div class="media-row" data-id="${m.id}" data-index="${index}" tabindex="0" role="button" aria-label="${escapeHtml(m.file_name)}">
      <span class="row-icon">🎵</span>
      <div class="row-body">
        <div class="row-title">${escapeHtml(m.file_name)}</div>
        <div class="row-meta">${escapeHtml(m.codec || '')} ${m.bitrate ? Math.round(m.bitrate/1000)+'kbps' : ''}</div>
      </div>
      <span class="row-duration">${fmtDur(m.duration)}</span>
      <button class="icon-btn btn-sm" data-action="play" title="Play">▶</button>
      <button class="icon-btn btn-sm" data-action="favorite" title="Favorite">♥</button>
      <button class="icon-btn btn-sm" data-action="notes" title="Notes">📝</button>
    </div>
  `;
}

function playSelected() {
  const el = currentElement();
  if (!el) return;
  const idx = parseInt(el.dataset.index, 10);
  const media = state.media[idx];
  if (media) selectAndPlay(media, idx);
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
