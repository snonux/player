// podcastUI.js — Podcast feed manager + episode rendering.
import { API } from './api.js';
import { state } from './state.js';
import { escapeHtml, fmtDur, toast } from './utils.js';
import { renderSets } from './views/sets.js';

export function initPodcasts() {
  const modal = document.getElementById('podcast-modal');
  const closeBtn = document.getElementById('podcast-close');
  const form = document.getElementById('podcast-form');
  const urlInput = document.getElementById('podcast-url');
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
    if (!url) return;
    try {
      await API.subscribePodcast(url, '');
      toast('Subscribed to podcast');
      urlInput.value = '';
      refreshPodcasts();
      refreshSets();
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
        `<div class="podcast-feed-row py-1 border-b">
          <img class="podcast-feed-cover" src="${escapeHtml(p.image_url || '/favicon.svg')}" alt="" loading="lazy">
          <span class="flex-1">${escapeHtml(p.title || p.feed_url)}</span>
          <span class="text-xs text-muted">${escapeHtml(p.feed_url)}</span>
        </div>`
      ).join('');
    } catch (err) {
      listEl.innerHTML = `<p class="error-message">${escapeHtml(err.message)}</p>`;
    }
  }

  async function refreshSets() {
    try {
      state.sets = await API.sets() || [];
      renderSets();
    } catch {
      // The subscription succeeded; leave the existing sidebar in place if refresh fails.
    }
  }
}

export function renderPodcastEpisodes(grid, episodes, options = {}) {
  if (!episodes || !episodes.length) return;
  const append = (node) => {
    if (options.before) {
      grid.insertBefore(node, options.before);
    } else {
      grid.appendChild(node);
    }
  };
  const divider = document.createElement('div');
  divider.className = 'grid-divider';
  divider.textContent = 'Podcast Episodes';
  append(divider);

  episodes.forEach(ep => {
    const card = document.createElement('div');
    card.className = 'media-card episode-card';
    card.dataset.id = ep.id;
    renderEpisodeCard(card, ep);
    append(card);
  });
}

function renderEpisodeCard(card, ep) {
  card.innerHTML = renderEpisodeHtml(ep);

  const playBtn = card.querySelector('[data-action="play"]');
  const downloadBtn = card.querySelector('[data-action="download-episode"]');
  const completeBtn = card.querySelector('.btn-complete');

  playBtn?.addEventListener('click', (e) => {
    e.stopPropagation();
    toast(ep.is_downloaded ? 'Play from downloads' : 'Download first to play', 'info');
  });

  downloadBtn?.addEventListener('click', async (e) => {
    e.stopPropagation();
    downloadBtn.disabled = true;
    try {
      const media = await API.downloadEpisode(ep.id);
      ep.is_downloaded = true;
      ep.media_id = media.id;
      ep.file_name = media.file_name;
      toast('Episode downloaded');
      card.dispatchEvent(new CustomEvent('podcast:episode-downloaded', { bubbles: true, detail: { media } }));
      renderEpisodeCard(card, ep);
    } catch (err) {
      downloadBtn.disabled = false;
      toast(err.message || 'Download failed', 'error');
    }
  });

  completeBtn?.addEventListener('click', async (e) => {
    e.stopPropagation();
    try {
      await API.toggleEpisodeComplete(ep.id);
      ep.is_completed = !ep.is_completed;
      completeBtn.classList.toggle('active', ep.is_completed);
      toast(ep.is_completed ? 'Marked listened' : 'Marked unlistened');
    } catch (err) {
      toast(err.message || 'Toggle failed', 'error');
    }
  });
}

function renderEpisodeHtml(ep) {
  const completed = ep.is_completed;
  const dateStr = ep.published_at ? new Date(ep.published_at).toLocaleDateString() : '';
  const duration = ep.duration_seconds ? fmtDur(ep.duration_seconds) : '';
  const downloadButton = ep.is_downloaded
    ? '<span class="text-xs text-muted">Downloaded</span>'
    : '<button class="btn btn-primary btn-sm btn-download-episode" data-action="download-episode">Download</button>';
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
      ${downloadButton}
    </div>
  `;
}
