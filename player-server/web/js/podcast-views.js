// podcast-views.js — subscription folders and All podcast episodes (§6.3).
// Uses feed_key identity, completion filters, and server pagination. Keeps
// downloaded episodes as podcast cards (server_copy) instead of dropping
// them into generic media cards.
import { API } from './api.js';
import { state, setMedia } from './state.js';
import { escapeHtml, fmtDur, toast } from './utils.js';
import { setEpisodeCompleted } from './podcast-episode-state.js';
import { clearSelection, selectByElement } from './selection.js';
import { dismissSidebarAfterNav } from './views/help.js';

// episodePlayableMedia builds a state.media slot for playMediaById /
// playSelected. Subscription views clear the browse list, so Play must put
// downloaded copies (or a fresh download) into state.media by card index.
export function episodePlayableMedia(ep, downloaded = null) {
  if (downloaded?.id) return downloaded;
  const id = ep?.server_copy?.media_id;
  if (!id) return null;
  return {
    id,
    type: 'audio',
    title: ep.title || '',
    is_podcast: true,
  };
}

export function mediaSlotsForEpisodes(episodes) {
  return (episodes || []).map((ep) => episodePlayableMedia(ep));
}

// rememberEpisodeMedia writes a playable row at the card's index so
// playMediaById (which only searches state.media) can start playback.
export function rememberEpisodeMedia(index, media) {
  if (!media?.id || !Number.isInteger(index) || index < 0) return;
  const list = state.media.slice();
  while (list.length <= index) list.push(null);
  list[index] = media;
  setMedia(list);
}

const FILTERS = [
  { id: 'uncompleted', label: 'Uncompleted' },
  { id: 'completed', label: 'Completed' },
  { id: 'all', label: 'All' },
];

let loadMediaCallback = () => {};
let playMediaById = async () => false;
let pageSize = 100;
let sidebarFeeds = [];

export function initPodcastViews({ onLoadMedia, playMediaById: playFn, pageSize: size } = {}) {
  loadMediaCallback = onLoadMedia || (() => {});
  playMediaById = playFn || (async () => false);
  setPodcastViewPageSize(size);
  ensureSidebar();
  refreshPodcastSidebar().catch(() => {});
  document.addEventListener('library:changed', () => {
    refreshPodcastSidebar().catch(() => {});
  });
}

export function setPodcastViewPageSize(size) {
  const parsed = Number.parseInt(size, 10);
  if (Number.isFinite(parsed) && parsed > 0) pageSize = parsed;
}

export function isPodcastVirtualView() {
  return state.virtualSet === 'podcasts';
}

export function openAllPodcastEpisodes() {
  openPodcastView('');
}

export function openPodcastFeed(feedKey) {
  if (!feedKey) return;
  openPodcastView(feedKey);
}

function openPodcastView(feedKey) {
  state.music = null;
  state.virtualSet = 'podcasts';
  state.podcastFeedKey = feedKey || '';
  if (!state.podcastFilter) state.podcastFilter = 'uncompleted';
  state.selectedSetId = null;
  state.selectedSetIds = [];
  state.folderPath = '';
  state.mediaPage = 0;
  updatePodcastSidebarUI();
  dismissSidebarAfterNav();
  loadMediaCallback();
}

export function clearPodcastView() {
  if (state.virtualSet !== 'podcasts') return;
  state.virtualSet = '';
  state.podcastFeedKey = '';
}

export function setPodcastFilter(filter) {
  if (!FILTERS.some((f) => f.id === filter)) return;
  state.podcastFilter = filter;
  state.mediaPage = 0;
  loadMediaCallback();
}

export async function refreshPodcastSidebar() {
  const list = document.getElementById('podcast-nav-list');
  if (!list) return;
  try {
    const podcasts = await API.podcasts() || [];
    sidebarFeeds = podcasts.filter((p) => p.subscribed);
    list.innerHTML = sidebarFeeds.length
      ? sidebarFeeds.map((p) => {
        const name = escapeHtml(p.title || p.feed_url || p.feed_key);
        return `<button type="button" class="btn btn-ghost podcast-nav-feed" data-feed-key="${escapeHtml(p.feed_key || '')}" aria-label="Podcast ${name}">${name}</button>`;
      }).join('')
      : '<p class="text-muted text-sm">No subscriptions yet.</p>';
    list.querySelectorAll('[data-feed-key]').forEach((btn) => {
      btn.addEventListener('click', () => openPodcastFeed(btn.dataset.feedKey));
    });
    updatePodcastSidebarUI();
  } catch (err) {
    list.innerHTML = `<p class="error-message text-sm">${escapeHtml(err.message || 'Failed to load')}</p>`;
  }
}

export function updatePodcastSidebarUI() {
  const allBtn = document.getElementById('podcast-all-episodes');
  allBtn?.classList.toggle('active', isPodcastVirtualView() && !state.podcastFeedKey);
  document.querySelectorAll('#podcast-nav-list [data-feed-key]').forEach((btn) => {
    btn.classList.toggle('active', isPodcastVirtualView() && btn.dataset.feedKey === state.podcastFeedKey);
  });
}

function ensureSidebar() {
  const sidebar = document.getElementById('sidebar');
  if (!sidebar || document.getElementById('podcast-nav')) return;
  const section = document.createElement('div');
  section.id = 'podcast-nav';
  section.innerHTML = `
    <div class="sidebar-header"><h2>Podcasts</h2></div>
    <button id="podcast-all-episodes" type="button" class="btn btn-ghost" aria-label="All podcast episodes">All podcast episodes</button>
    <div id="podcast-nav-list" class="podcast-nav-list" aria-label="Subscribed podcasts"></div>`;
  const setList = document.getElementById('set-list');
  if (setList) sidebar.insertBefore(section, setList);
  else sidebar.appendChild(section);
  document.getElementById('podcast-all-episodes')?.addEventListener('click', openAllPodcastEpisodes);
}

export async function renderPodcastVirtualView(grid, { revision, alive, pageSize: size } = {}) {
  if (!grid) return null;
  setPodcastViewPageSize(size);
  breadcrumbHidden(true);
  const filter = state.podcastFilter || 'uncompleted';
  const feedKey = state.podcastFeedKey || '';
  const page = Math.max(0, state.mediaPage | 0);
  const offset = page * pageSize;
  const title = feedKey
    ? (sidebarFeeds.find((f) => f.feed_key === feedKey)?.title || feedKey)
    : 'All podcast episodes';

  let episodes = [];
  try {
    episodes = await API.subscribedEpisodes({ feedKey, filter, limit: pageSize, offset }) || [];
  } catch (err) {
    if (alive && !alive()) return null;
    grid.innerHTML = `<p class="error-message grid-full">${escapeHtml(err.message)}</p>`;
    return null;
  }
  if (alive && !alive()) return null;
  if (revision !== undefined && typeof alive === 'function' && !alive()) return null;

  const hasNext = episodes.length >= pageSize;
  const hasPrev = page > 0;
  const start = episodes.length ? offset + 1 : 0;
  const end = offset + episodes.length;
  const pageInfo = {
    items: episodes,
    total: hasNext ? end + 1 : end,
    page,
    pageSize,
    start: episodes.length ? start - 1 : 0,
    end,
    hasPrev,
    hasNext,
    approx: hasNext,
  };

  grid.innerHTML = filterBarHtml(title, filter)
    + pagerHtml(pageInfo, 'top')
    + (episodes.length ? '' : `<p class="text-muted text-sm grid-full">No episodes.</p>`)
    + pagerHtml(pageInfo, 'bottom');

  // Align state.media with episode card indices so Play / Space / Enter can
  // resolve downloaded copies the same way set-browse playMediaById does.
  setMedia(mediaSlotsForEpisodes(episodes));

  const before = grid.querySelector('[data-page-pager="bottom"]');
  episodes.forEach((ep, index) => {
    const card = document.createElement('div');
    card.className = 'media-card episode-card';
    card.dataset.episodeId = String(ep.id);
    card.dataset.feedKey = ep.feed_key || '';
    card.dataset.episodeKey = ep.episode_key || '';
    card.dataset.index = String(index);
    const mediaId = ep.server_copy?.media_id;
    if (mediaId) card.dataset.id = String(mediaId);
    card.tabIndex = -1;
    renderViewEpisodeCard(card, ep, { showFeed: !feedKey });
    if (before) grid.insertBefore(card, before);
    else grid.appendChild(card);
  });

  bindFilterBar(grid);
  bindPager(grid, pageInfo);
  clearSelection();
  updatePodcastSidebarUI();
  return pageInfo;
}

function filterBarHtml(title, filter) {
  const buttons = FILTERS.map((f) => {
    const pressed = f.id === filter;
    return `<button type="button" class="btn btn-sm podcast-filter-btn${pressed ? ' active' : ''}"
      data-podcast-filter="${f.id}" aria-pressed="${pressed}">${escapeHtml(f.label)}</button>`;
  }).join('');
  return `<div class="podcast-view-toolbar grid-full" role="region" aria-label="Podcast episodes">
    <h2 class="podcast-view-title">${escapeHtml(title)}</h2>
    <div class="podcast-filter-bar" role="group" aria-label="Completion filter">${buttons}</div>
  </div>`;
}

function pagerHtml(page, place) {
  if (!page.hasPrev && !page.hasNext) return '';
  return `<div class="grid-pager grid-full" data-page-pager="${place}">
    <button type="button" class="btn btn-sm" data-page-delta="-1" ${page.hasPrev ? '' : 'disabled'} aria-label="Previous page">← Prev</button>
    <span class="text-muted text-sm">Page ${page.page + 1}${page.approx ? '+' : ''}</span>
    <button type="button" class="btn btn-sm" data-page-delta="1" ${page.hasNext ? '' : 'disabled'} aria-label="Next page">Next →</button>
  </div>`;
}

function bindFilterBar(grid) {
  grid.querySelectorAll('[data-podcast-filter]').forEach((btn) => {
    btn.addEventListener('click', () => setPodcastFilter(btn.dataset.podcastFilter));
  });
}

function bindPager(grid, pageInfo) {
  grid.querySelectorAll('[data-page-delta]').forEach((btn) => {
    btn.addEventListener('click', () => {
      const delta = parseInt(btn.dataset.pageDelta, 10);
      if (!Number.isFinite(delta)) return;
      const next = pageInfo.page + delta;
      if (next < 0) return;
      if (delta > 0 && !pageInfo.hasNext) return;
      state.mediaPage = next;
      loadMediaCallback();
    });
  });
}

function renderViewEpisodeCard(card, ep, { showFeed } = {}) {
  card.innerHTML = episodeCardHtml(ep, { showFeed });
  const playBtn = card.querySelector('[data-action="play"]');
  const downloadBtn = card.querySelector('[data-action="download-episode"]');
  const completeBtn = card.querySelector('[data-action="complete"]');

  playBtn?.addEventListener('click', (e) => {
    e.stopPropagation();
    playEpisode(card, ep, [playBtn, downloadBtn].filter(Boolean));
  });
  downloadBtn?.addEventListener('click', (e) => {
    e.stopPropagation();
    downloadEpisodeOnly(card, ep, [playBtn, downloadBtn].filter(Boolean));
  });
  completeBtn?.addEventListener('click', async (e) => {
    e.stopPropagation();
    await markCompletion(card, ep, !ep.completed);
  });
}

function episodeCardHtml(ep, { showFeed } = {}) {
  const completed = Boolean(ep.completed);
  const downloaded = Boolean(ep.server_copy?.media_id);
  const dateStr = ep.published_at ? new Date(ep.published_at).toLocaleDateString() : '';
  const duration = ep.duration_seconds ? fmtDur(ep.duration_seconds) : '';
  const position = ep.position_seconds > 0 ? `Resume ${fmtDur(ep.position_seconds)}` : '';
  const downloadState = downloaded ? 'Downloaded on server' : 'Not downloaded';
  const completeLabel = completed ? 'Mark uncompleted' : 'Mark completed';
  const feedLine = showFeed && ep.feed_title
    ? `<div class="subtitle">${escapeHtml(ep.feed_title)}</div>`
    : '';
  const metaBits = [dateStr, duration, position, downloadState].filter(Boolean).join(' · ');
  const downloadBtn = downloaded
    ? ''
    : '<button type="button" class="icon-btn btn-sm btn-download-episode" data-action="download-episode" title="Download to server" aria-label="Download episode to server">↓</button>';
  const playLabel = downloaded ? 'Play' : 'Download and play';
  return `
    <div class="thumb-wrap">
      <span class="placeholder">🎙️</span>
      <span class="badge">${escapeHtml(metaBits)}</span>
      <div class="card-actions">
        ${downloadBtn}
        <button type="button" class="icon-btn btn-sm" data-action="play" title="${playLabel}" aria-label="${playLabel} episode">▶</button>
        <button type="button" class="icon-btn btn-sm btn-complete${completed ? ' active' : ''}" data-action="complete"
          title="${completeLabel}" aria-label="${completeLabel}" aria-pressed="${completed}">✓</button>
      </div>
    </div>
    <div class="meta">
      <div class="title">${escapeHtml(ep.title || 'Untitled')}</div>
      ${feedLine}
      <div class="subtitle">${escapeHtml(ep.description || '')}</div>
    </div>`;
}

async function playEpisode(card, ep, buttons) {
  const index = parseInt(card.dataset.index, 10);
  let media = Number.isInteger(index) ? state.media[index] : null;
  if (media?.id) {
    card.dataset.id = String(media.id);
    if (!(await playMediaById(media.id))) toast('Select the episode to play', 'info');
    return;
  }
  const mediaId = ep.server_copy?.media_id;
  if (mediaId) {
    // Detail fills fields playMediaById / the player need when the slot was
    // empty (e.g. after a filter refresh race).
    try {
      media = (await API.mediaDetail(mediaId))?.media || episodePlayableMedia(ep);
    } catch {
      media = episodePlayableMedia(ep);
    }
    rememberEpisodeMedia(index, media);
    card.dataset.id = String(media.id);
    if (!(await playMediaById(media.id))) toast('Select the episode to play', 'info');
    return;
  }
  buttons.forEach((b) => { b.disabled = true; });
  toast('Downloading episode…', 'info');
  try {
    media = await API.downloadEpisode(ep.id);
    ep.server_copy = { media_id: media.id };
    rememberEpisodeMedia(index, media);
    card.dataset.id = String(media.id);
    toast('Episode downloaded');
    renderViewEpisodeCard(card, ep, { showFeed: !state.podcastFeedKey });
    if (!(await playMediaById(media.id))) toast('Episode downloaded; select it to play', 'info');
  } catch (err) {
    buttons.forEach((b) => { b.disabled = false; });
    toast(err.message || 'Download failed', 'error');
  }
}

async function downloadEpisodeOnly(card, ep, buttons) {
  const index = parseInt(card.dataset.index, 10);
  buttons.forEach((b) => { b.disabled = true; });
  try {
    const media = await API.downloadEpisode(ep.id);
    ep.server_copy = { media_id: media.id };
    rememberEpisodeMedia(index, media);
    card.dataset.id = String(media.id);
    toast('Episode downloaded');
    renderViewEpisodeCard(card, ep, { showFeed: !state.podcastFeedKey });
  } catch (err) {
    buttons.forEach((b) => { b.disabled = false; });
    toast(err.message || 'Download failed', 'error');
  }
}

async function markCompletion(card, ep, completed) {
  const btn = card.querySelector('[data-action="complete"]');
  if (btn) btn.disabled = true;
  try {
    await setEpisodeCompleted(ep, completed);
    ep.completed = completed;
    toast(completed ? 'Marked completed' : 'Marked uncompleted');
    const filter = state.podcastFilter || 'uncompleted';
    const leaves = (filter === 'uncompleted' && completed) || (filter === 'completed' && !completed);
    if (leaves) {
      focusAfterRemoval(card);
      card.remove();
      if (!document.querySelector('#media-grid .episode-card')) loadMediaCallback();
      return;
    }
    renderViewEpisodeCard(card, ep, { showFeed: !state.podcastFeedKey });
  } catch (err) {
    if (btn) btn.disabled = false;
    toast(err.message || 'Update failed', 'error');
  }
}

function focusAfterRemoval(card) {
  const cards = Array.from(document.querySelectorAll('#media-grid .episode-card'));
  const idx = cards.indexOf(card);
  const neighbor = cards[idx + 1] || cards[idx - 1]
    || document.querySelector('.podcast-filter-bar [aria-pressed="true"]')
    || document.getElementById('podcast-all-episodes');
  if (neighbor?.classList?.contains('episode-card')) selectByElement(neighbor);
  else neighbor?.focus?.();
}

function breadcrumbHidden(hidden) {
  document.getElementById('breadcrumb-bar')?.classList.toggle('hidden', hidden);
}
