// podcastUI.js — Podcast feed manager + episode rendering.
import { API } from './api.js';
import { state } from './state.js';
import { escapeHtml, fmtDur, toast } from './utils.js';
import { renderSets } from './views/sets.js';

export function initPodcasts() {
  const modal = document.getElementById('podcast-modal');
  const form = document.getElementById('podcast-form');
  const urlInput = document.getElementById('podcast-url');
  if (!modal) return;

  document.getElementById('admin-podcasts')?.addEventListener('click', () => {
    modal.classList.add('open');
    refreshPodcasts();
  });
  document.getElementById('podcast-close')?.addEventListener('click', () => modal.classList.remove('open'));
  modal.addEventListener('click', (e) => { if (e.target === modal) modal.classList.remove('open'); });

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
}

async function refreshPodcasts() {
  const listEl = document.getElementById('podcast-list');
  if (!listEl) return;
  try {
    const podcasts = await API.podcasts();
    if (!podcasts || !podcasts.length) {
      listEl.innerHTML = '<p class="text-muted text-sm">No podcasts subscribed.</p>';
      return;
    }
    listEl.innerHTML = podcasts.map(feedRowHtml).join('');
    listEl.querySelectorAll('[data-unsubscribe]').forEach((button) => {
      const feed = podcasts.find((p) => String(p.id) === button.dataset.unsubscribe);
      button.addEventListener('click', () => unsubscribe(feed, button));
    });
  } catch (err) {
    listEl.innerHTML = `<p class="error-message">${escapeHtml(err.message)}</p>`;
  }
}

function feedRowHtml(p) {
  const name = escapeHtml(p.title || p.feed_url);
  return `<div class="podcast-feed-row py-1 border-b">
    <img class="podcast-feed-cover" src="${escapeHtml(p.image_url || '/favicon.svg')}" alt="" loading="lazy">
    <span class="flex-1">${name}</span>
    <span class="text-xs text-muted">${escapeHtml(p.feed_url)}</span>
    <button type="button" class="btn btn-danger btn-sm" data-unsubscribe="${p.id}"
      aria-label="Unsubscribe from ${name}">Unsubscribe</button>
  </div>`;
}

// Unsubscribing deletes downloaded episodes from the server, so it needs
// an explicit confirmation naming the feed.
async function unsubscribe(feed, button) {
  const name = feed?.title || feed?.feed_url || 'this podcast';
  if (!feed || !confirm(`Unsubscribe from "${name}"? Its downloaded episodes are deleted, including everyone's notes, progress, favorites and shares for them. Its cover art goes too unless another feed or other library media still uses the folder; other files are kept.`)) return;
  button.disabled = true;
  try {
    await API.unsubscribePodcast(feed.id);
    toast('Unsubscribed');
    await refreshPodcasts();
    refreshSets();
    // The grid may be showing the removed feed's folder.
    document.dispatchEvent(new CustomEvent('library:changed'));
  } catch (err) {
    button.disabled = false;
    if (document.activeElement === document.body) button.focus();
    toast(err.message || 'Unsubscribe failed', 'error');
  }
}

async function refreshSets() {
  try {
    state.sets = await API.sets() || [];
    renderSets();
  } catch {
    // The feed change succeeded; leave the existing sidebar in place if refresh fails.
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
    // Not data-id: media shortcuts (F, t, s, D, n) read data-id as a media
    // ID, and an episode ID would make them act on an unrelated item.
    card.dataset.episodeId = ep.id;
    renderEpisodeCard(card, ep);
    append(card);
  });
}

function renderEpisodeCard(card, ep) {
  card.innerHTML = renderEpisodeHtml(ep);

  const playBtn = card.querySelector('[data-action="play"]');
  const downloadBtn = card.querySelector('[data-action="download-episode"]');
  const completeBtn = card.querySelector('.btn-complete');

  // Episode cards only exist for episodes not yet on the server, so Play
  // downloads first and then asks the grid to start the new media.
  playBtn?.addEventListener('click', (e) => {
    e.stopPropagation();
    downloadEpisode(card, ep, [playBtn, downloadBtn], true);
  });

  downloadBtn?.addEventListener('click', (e) => {
    e.stopPropagation();
    downloadEpisode(card, ep, [playBtn, downloadBtn], false);
  });

  completeBtn?.addEventListener('click', async (e) => {
    e.stopPropagation();
    try {
      await API.toggleEpisodeComplete(ep.id);
      ep.is_completed = !ep.is_completed;
      updateCompleteButton(completeBtn, ep.is_completed);
      toast(ep.is_completed ? 'Marked listened' : 'Marked unlistened');
    } catch (err) {
      toast(err.message || 'Toggle failed', 'error');
    }
  });
}

// downloadEpisode stores the episode on the server. On success the grid
// reloads (the episode becomes a normal media card) and, when play is set,
// starts that media. Buttons stay disabled while the request runs so a
// double click cannot start two downloads.
async function downloadEpisode(card, ep, buttons, play) {
  buttons.forEach((b) => { if (b) b.disabled = true; });
  if (play) toast('Downloading episode…', 'info');
  try {
    const media = await API.downloadEpisode(ep.id);
    ep.is_downloaded = true;
    ep.media_id = media.id;
    ep.file_name = media.file_name;
    toast('Episode downloaded');
    card.dispatchEvent(new CustomEvent('podcast:episode-downloaded', { bubbles: true, detail: { media, play } }));
    renderEpisodeCard(card, ep);
  } catch (err) {
    buttons.forEach((b) => { if (b) b.disabled = false; });
    toast(err.message || 'Download failed', 'error');
  }
}

function updateCompleteButton(button, completed) {
  const label = completed ? 'Mark as unlistened' : 'Mark as listened';
  button.classList.toggle('active', completed);
  button.title = label;
  button.setAttribute('aria-label', label);
  button.setAttribute('aria-pressed', String(completed));
}

function renderEpisodeHtml(ep) {
  const completed = ep.is_completed;
  const dateStr = ep.published_at ? new Date(ep.published_at).toLocaleDateString() : '';
  const duration = ep.duration_seconds ? fmtDur(ep.duration_seconds) : '';
  const downloadButton = ep.is_downloaded
    ? '<span class="text-xs text-muted">Downloaded</span>'
    : '<button class="btn btn-primary btn-sm btn-download-episode" data-action="download-episode" aria-label="Download episode">Download</button>';
  const completeLabel = completed ? 'Mark as unlistened' : 'Mark as listened';
  return `
    <div class="thumb-wrap">
      <span class="placeholder">🎙️</span>
      <span class="badge">${dateStr}${duration ? ' • ' + duration : ''}</span>
      <div class="card-actions">
        <button class="icon-btn btn-sm" data-action="play" title="Download and play" aria-label="Download and play episode">▶</button>
        <button class="icon-btn btn-sm btn-complete${completed ? ' active' : ''}" title="${completeLabel}" aria-label="${completeLabel}" aria-pressed="${completed}">✓</button>
      </div>
    </div>
    <div class="meta">
      <div class="title">${escapeHtml(ep.title || 'Untitled')}</div>
      <div class="subtitle">${escapeHtml(ep.description || 'Podcast episode')}</div>
      ${downloadButton}
    </div>
  `;
}
