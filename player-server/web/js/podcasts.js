import { podcastStreamMedia } from './podcast-playback.js';
import { podcastDownloading, downloadPodcastEpisode, cancelPodcastDownload } from './podcast-downloads.js';
import { bindPodcastMetadata, episodeCardMarkup } from './podcast-metadata.js';
// podcastUI.js — Podcast feed manager + episode rendering.
import { API } from './api.js';
import { state, setMedia } from './state.js';
import { escapeHtml, toast } from './utils.js';
import { renderSets } from './views/sets.js';
import { setSubscribed } from './podcast-subscriptions.js';
import { setEpisodeCompleted, podcastSyncStatus } from './podcast-episode-state.js';

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
      document.dispatchEvent(new CustomEvent('library:changed'));
    } catch (err) {
      toast(err.message || 'Subscribe failed', 'error');
    }
  });
}

// refreshPodcasts lists the server's catalog feeds in two groups: those the
// signed-in user is subscribed to, and those still on the server without
// the user's subscription. Re-rendering keeps keyboard focus in place
// through modal-focus.js.
async function refreshPodcasts() {
  const listEl = document.getElementById('podcast-list');
  if (!listEl) return;
  try {
    const podcasts = await API.podcasts() || [];
    if (!podcasts.length) {
      listEl.innerHTML = '<p class="text-muted text-sm">No podcasts on the server.</p>';
      return;
    }
    const subscribed = podcasts.filter((p) => p.subscribed);
    const others = podcasts.filter((p) => !p.subscribed);
    listEl.innerHTML = feedGroupHtml('Subscribed', subscribed, 'You are not subscribed to any podcast.')
      + (others.length ? feedGroupHtml('Not subscribed (still on the server)', others, '') : '');
    listEl.querySelectorAll('[data-feed-action]').forEach((button) => {
      const feed = podcasts.find((p) => String(p.id) === button.dataset.feedId);
      button.addEventListener('click', () => feedAction(button.dataset.feedAction, feed, button));
    });
  } catch (err) {
    listEl.innerHTML = `<p class="error-message">${escapeHtml(err.message)}</p>`;
  }
}

function feedGroupHtml(title, feeds, emptyText) {
  const rows = feeds.length ? feeds.map(feedRowHtml).join('') : `<p class="text-muted text-sm">${escapeHtml(emptyText)}</p>`;
  return `<h5 class="my-1 text-90">${escapeHtml(title)}</h5>${rows}`;
}

function feedRowHtml(p) {
  const name = escapeHtml(p.title || p.feed_url);
  const toggle = p.subscribed
    ? `<button type="button" class="btn btn-sm" data-feed-action="unsubscribe" data-feed-id="${p.id}"
      aria-label="Unsubscribe from ${name}">Unsubscribe</button>`
    : `<button type="button" class="btn btn-primary btn-sm" data-feed-action="subscribe" data-feed-id="${p.id}"
      aria-label="Subscribe to ${name}">Subscribe</button>`;
  return `<div class="podcast-feed-row py-1 border-b" data-feed-key="${escapeHtml(p.feed_key || '')}">
    <img class="podcast-feed-cover" src="${escapeHtml(p.image_url || '/favicon.svg')}" alt="" loading="lazy">
    <span class="flex-1">${name}</span>
    <span class="text-xs text-muted">${escapeHtml(p.feed_url)}</span>
    ${toggle}
    <button type="button" class="btn btn-danger btn-sm" data-feed-action="remove" data-feed-id="${p.id}"
      aria-label="Remove ${name} from the server">Remove from server</button>
  </div>`;
}

async function feedAction(action, feed, button) {
  if (!feed) return;
  if (action === 'remove') {
    await removeFromServer(feed, button);
    return;
  }
  const subscribe = action === 'subscribe';
  button.disabled = true;
  try {
    await setSubscribed(feed, subscribe);
    toast(subscribe ? 'Subscribed' : 'Unsubscribed');
    await refreshPodcasts();
    document.dispatchEvent(new CustomEvent('library:changed'));
  } catch (err) {
    restoreButton(button);
    toast(err.message || (subscribe ? 'Subscribe failed' : 'Unsubscribe failed'), 'error');
  }
}

// Removing from the server deletes the feed's downloaded episodes for every
// user, so it needs an explicit confirmation naming the feed and the scope.
async function removeFromServer(feed, button) {
  const name = feed.title || feed.feed_url || 'this podcast';
  if (!confirm(`Remove "${name}" from the server? This deletes its episodes downloaded to the server for everyone, including everyone's notes, bookmarks, favorites and shares for them. Its cover art goes too unless another feed or other library media still uses the folder; other files are kept. Subscriptions and listening history stay; while anyone is still subscribed the feed itself stays listed.`)) return;
  button.disabled = true;
  try {
    await API.removePodcastFromServer(feed.id);
    toast('Removed from the server');
    await refreshPodcasts();
    refreshSets();
    // The grid may be showing the removed feed's folder.
    document.dispatchEvent(new CustomEvent('library:changed'));
  } catch (err) {
    restoreButton(button);
    toast(err.message || 'Remove failed', 'error');
  }
}

function restoreButton(button) {
  button.disabled = false;
  if (document.activeElement === document.body) button.focus();
}

async function refreshSets() {
  try {
    state.sets = await API.sets() || [];
    renderSets();
  } catch {
    // The feed change succeeded; leave the existing sidebar in place if refresh fails.
  }
}

const renderedEpisodes = new WeakMap();
globalThis.document?.addEventListener?.('podcast:download-changed', event => {
  for (const card of document.querySelectorAll('.episode-card[data-episode-id]')) {
    const ep = renderedEpisodes.get(card);
    if (ep && String(ep.id) === String(event.detail.id)) {
      if (event.detail.media) {
        ep.is_downloaded = true;
        ep.media_id = event.detail.media.id;
        ep.file_name = event.detail.media.file_name;
        card.dispatchEvent(new CustomEvent('podcast:episode-downloaded', { bubbles: true, detail: { media: event.detail.media, play: false } }));
      }
      renderEpisodeCard(card, ep);
    }
  }
});

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
  const focusedAction = card.contains(document.activeElement) ? document.activeElement?.dataset.action : null;
  renderedEpisodes.set(card, ep);
  const downloading = podcastDownloading(ep.id);
  card.innerHTML = episodeSetFolderCardHtml(ep, { downloading });
  bindPodcastMetadata(card);

  const playBtn = card.querySelector('[data-action="play"]');
  const downloadBtn = card.querySelector('[data-action="download-episode"]');
  const cancelBtn = card.querySelector('[data-action="cancel-download"]');
  const completeBtn = card.querySelector('[data-action="complete"]');
  const moreBtn = card.querySelector('[data-action="more"]');

  // Publisher playback is independent of the saved-copy transfer, so Play
  // remains available while Download is running or after it is cancelled.
  playBtn?.addEventListener('click', (e) => {
    e.stopPropagation();
    const media = podcastStreamMedia(ep);
    if (!media) return;
    const index = state.media.length;
    setMedia([...state.media, media]);
    card.dataset.id = String(media.id);
    card.dataset.index = String(index);
    card.dispatchEvent(new CustomEvent('podcast:episode-stream', { bubbles: true, detail: { media } }));
  });
  downloadBtn?.addEventListener('click', (e) => {
    e.stopPropagation();
    downloadEpisode(card, ep);
  });
  cancelBtn?.addEventListener('click', (e) => {
    e.stopPropagation();
    cancelPodcastDownload(ep.id);
  });
  completeBtn?.addEventListener('click', async (e) => {
    e.stopPropagation();
    try {
      const requested = !ep.is_completed;
      const result = await setEpisodeCompleted(ep, requested);
      ep.is_completed = result?.completion?.completed ?? ep.is_completed;
      toast(ep.is_completed === requested
        ? (ep.is_completed ? 'Marked completed' : 'Marked uncompleted')
        : 'A newer completion change was kept');
      renderEpisodeCard(card, ep);
    } catch (err) {
      toast(err.message || 'Toggle failed', 'error');
    }
  });
  moreBtn?.addEventListener('click', (e) => {
    e.stopPropagation();
    document.dispatchEvent(new CustomEvent('media:actions', { detail: card }));
  });
  if (focusedAction) {
    (card.querySelector(`[data-action="${focusedAction}"]`) || cancelBtn || playBtn || downloadBtn)?.focus();
  }
}

// Stores the episode on the server. Cancel uses AbortController; More keeps
// completion reachable while Cancel is the primary control.
async function downloadEpisode(card, ep) {
  if (podcastDownloading(ep.id)) return;
  const lease = podcastSyncStatus().generation;
  try {
    const media = await downloadPodcastEpisode(ep.id);
    if (podcastSyncStatus().generation !== lease) return;
    ep.is_downloaded = true;
    ep.media_id = media.id;
    ep.file_name = media.file_name;
    toast('Episode downloaded');
    if (card.isConnected) {
      renderEpisodeCard(card, ep);
    }
  } catch (err) {
    if (podcastSyncStatus().generation !== lease) return;
    toast(err?.name === 'AbortError' ? 'Download canceled' : err.message || 'Download failed', err?.name === 'AbortError' ? 'info' : 'error');
  }
}

// Set-folder episode card (see episodeCardMarkup).
export function episodeSetFolderCardHtml(ep, { downloading = false } = {}) {
  return episodeCardMarkup(ep, {
    completed: Boolean(ep.is_completed),
    downloaded: Boolean(ep.is_downloaded),
    downloading,
    downloadLabel: { title: 'Download episode', aria: 'Download episode' },
  });
}
