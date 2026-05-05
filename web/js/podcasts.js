// podcastUI.js — Podcast feed manager + episode rendering.
import { API } from './api.js';

export function initPodcasts() {
  const modal = document.getElementById('podcast-modal');
  const closeBtn = document.getElementById('podcast-close');
  const form = document.getElementById('podcast-form');
  const urlInput = document.getElementById('podcast-url');
  const nameInput = document.getElementById('podcast-name');
  const listEl = document.getElementById('podcast-list');
  const adminBtn = document.getElementById('admin-podcasts');

  if (!modal) return;

  adminBtn?.addEventListener('click', () => {
    modal.classList.add('open');
    refreshPodcasts();
  });

  closeBtn?.addEventListener('click', () => modal.classList.remove('open'));
  modal?.addEventListener('click', (e) => { if (e.target === modal) modal.classList.remove('open'); });

  form?.addEventListener('submit', async (e) => {
    e.preventDefault();
    const url = urlInput.value.trim();
    const name = nameInput.value.trim();
    if (!url) return;
    try {
      await API.subscribePodcast(url, name);
      toast('Subscribed to podcast');
      urlInput.value = '';
      nameInput.value = '';
      refreshPodcasts();
    } catch (err) {
      toast(err.message || 'Subscribe failed', 'error');
    }
  });

  async function refreshPodcasts() {
    if (!listEl) return;
    try {
      const podcasts = await API.podcasts();
      if (!podcasts || !podcasts.length) {
        listEl.innerHTML = '<p class="text-muted text-sm">No podcasts subscribed.</p>';
        return;
      }
      listEl.innerHTML = podcasts.map(p =>
        `<div class="flex gap-2 align-center py-1 border-b">
          <span class="flex-1">${escapeHtml(p.name)}</span>
          <span class="text-xs text-muted">${escapeHtml(p.root_path)}</span>
        </div>`
      ).join('');
    } catch (err) {
      listEl.innerHTML = `<p class="error-message">${escapeHtml(err.message)}</p>`;
    }
  }
}

export function renderPodcastEpisodes(grid, episodes) {
  if (!episodes || !episodes.length) return;
  const divider = document.createElement('div');
  divider.className = 'grid-divider';
  divider.textContent = 'Podcast Episodes';
  grid.appendChild(divider);

  episodes.forEach(ep => {
    const card = document.createElement('div');
    card.className = 'media-card episode-card';
    card.dataset.id = ep.id;
    card.innerHTML = renderEpisodeHtml(ep);
    grid.appendChild(card);

    const playBtn = card.querySelector('[data-action="play"]');
    const completeBtn = card.querySelector('.btn-complete');
    playBtn?.addEventListener('click', () => {
      toast(ep.is_downloaded ? 'Play from downloads' : 'Download first to play', 'info');
    });
    completeBtn?.addEventListener('click', async () => {
      try {
        const res = await API.toggleEpisodeComplete(ep.id);
        completeBtn.classList.toggle('active');
        toast(res.is_completed ? 'Marked listened' : 'Marked unlistened');
      } catch (err) { toast(err.message || 'Toggle failed', 'error'); }
    });
  });
}

function renderEpisodeHtml(ep) {
  const completed = ep.is_completed;
  const dateStr = ep.published_at ? new Date(ep.published_at).toLocaleDateString() : '';
  const duration = ep.duration_seconds ? fmtDuration(ep.duration_seconds) : '';
  return `
    <div class="thumb-wrap">
      <span class="placeholder">🎙️</span>
      <span class="badge">${dateStr}${duration ? ' • ' + duration : ''}</span>
      <div class="card-actions">
        <button class="icon-btn btn-sm" data-action="play" title="Play">▶</button>
        <button class="icon-btn btn-sm btn-complete${completed ? ' active' : ''}" title="Mark as listened">✓</button>
      </div>
    </div>
    <div class="meta">
      <div class="title">${escapeHtml(ep.title || 'Untitled')}</div>
      <div class="subtitle">${escapeHtml(ep.description || 'Podcast episode')}</div>
    </div>
  `;
}

function fmtDuration(s) {
  const m = Math.floor(s / 60);
  const h = Math.floor(m / 60);
  if (h > 0) return `${h}h ${m % 60}m`;
  return `${m}m`;
}

function escapeHtml(str) {
  if (!str) return '';
  return str.replace(/[&<>"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
}

function toast(msg, type = 'info') {
  const t = document.getElementById('toast');
  if (!t) return;
  t.textContent = msg;
  t.className = 'toast show ' + type;
  setTimeout(() => t.classList.remove('show'), 2800);
}
