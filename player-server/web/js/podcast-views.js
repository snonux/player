import { publicationDate, showNotesHTML, episodeArtworkHTML, bindPodcastMetadata } from './podcast-metadata.js';
import { openPodcastDiscovery } from './podcast-discovery.js';
import { setSubscribed, listAccountPodcastSubscriptions } from './podcast-subscriptions.js';
import { podcastFeedKey } from './podcast-identity.js';
// podcast-views.js — subscription folders and All podcast episodes (§6.3).
// Uses feed_key identity, completion filters, and server pagination. Keeps
// downloaded episodes as podcast cards (server_copy) instead of dropping
// them into generic media cards.
import { API } from './api.js';
import { state, setMedia } from './state.js';
import { escapeHtml, fmtDur, toast } from './utils.js';
import { setEpisodeCompleted, setEpisodeDeleted, episodeSnapshot, podcastSyncStatus, retryPodcastEpisodeEdits } from './podcast-episode-state.js';
import { clearSelection, selectByElement, currentElement } from './selection.js';
import { dismissSidebarAfterNav } from './views/help.js';

// episodePlayableMedia builds a state.media slot for playMediaById /
// playSelected. Subscription views clear the browse list, so Play must put
// downloaded copies (or a fresh download) into state.media by card index.
export function episodePlayableMedia(ep, downloaded = null) {
  if (downloaded?.id) return { ...downloaded, podcast_episode: episodeSnapshot(ep) };
  const id = ep?.server_copy?.media_id;
  if (!id) return null;
  return {
    id,
    type: 'audio',
    title: ep.title || '',
    is_podcast: true,
    podcast_episode: episodeSnapshot(ep),
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
let visibleEpisodes = [];
let stateUpdateTimer;
let sidebarRefreshRevision = 0;

export function initPodcastViews({ onLoadMedia, playMediaById: playFn, pageSize: size } = {}) {
  loadMediaCallback = onLoadMedia || (() => {});
  playMediaById = playFn || (async () => false);
  setPodcastViewPageSize(size);
  ensureSidebar();
  refreshPodcastSidebar().catch(() => {});
  document.addEventListener('library:changed', () => {
    refreshPodcastSidebar().catch(() => {});
  });
  document.addEventListener('podcast:subscriptions-changed', () => {
    refreshPodcastSidebar({ background: true }).catch(() => {});
  });
  document.addEventListener('podcast:state-changed', () => {
    // Let the ended handler select its next card before completion removes
    // the outgoing card from the current filter.
    clearTimeout(stateUpdateTimer);
    stateUpdateTimer = setTimeout(updateVisibleEpisodeState, 0);
  });
}

export function setPodcastViewPageSize(size) {
  const parsed = Number.parseInt(size, 10);
  if (Number.isFinite(parsed) && parsed > 0) pageSize = parsed;
}

export function isPodcastVirtualView() {
  return state.virtualSet === 'podcasts';
}

// Reuse the actual card actions, including download state and delete confirmation.
export function selectedPodcastEpisodeAction(action) {
  if (!isPodcastVirtualView()) return false;
  const card = currentElement();
  if (!card?.classList.contains('episode-card')) return false;
  const button = card.querySelector(`[data-action="${action}"]`);
  if (!button) return false;
  if (!button.disabled) button.click();
  return true;
}

export function openPodcastOverview() {
  openPodcastView('', true);
}

export function openAllPodcastEpisodes() {
  openPodcastView('');
}

export function openPodcastFeed(feedKey) {
  if (!feedKey) return;
  openPodcastView(feedKey);
}

function openPodcastView(feedKey, overview = false) {
  state.music = null;
  state.virtualSet = 'podcasts';
  state.podcastOverview = overview;
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

export async function refreshPodcastSidebar({ background = false } = {}) {
  const list = document.getElementById('podcast-nav-list');
  if (!list) return;
  const revision = ++sidebarRefreshRevision;
  const generation = podcastSyncStatus().generation;
  try {
    // Catalog metadata is optional: subscriptions confer no set permissions.
    const catalog = await API.podcasts({ quietAuth: background }).catch(error => {
      if (error.status === 401) throw error;
      return [];
    }) || [];
    const podcasts = await listAccountPodcastSubscriptions(catalog);
    if (revision !== sidebarRefreshRevision || generation !== podcastSyncStatus().generation) return;
    sidebarFeeds = podcasts.filter((p) => p.subscribed);
    const fingerprint = JSON.stringify(sidebarFeeds);
    if (list.dataset.syncFingerprint === fingerprint) { updatePodcastSidebarUI(); return; }
    const focusedKey = list.contains(document.activeElement) ? document.activeElement?.dataset.feedKey : null;
    list.dataset.syncFingerprint = fingerprint;
    list.innerHTML = sidebarFeeds.length
      ? sidebarFeeds.map((p) => {
        const name = escapeHtml(p.title || p.feed_url || p.feed_key);
        return `<button type="button" class="btn btn-ghost podcast-nav-feed" data-feed-key="${escapeHtml(p.feed_key || '')}" aria-label="Podcast ${name}" title="${name}">${name}</button>`;
      }).join('')
      : '<p class="text-muted text-sm">No subscriptions yet.</p>';
    list.querySelectorAll('[data-feed-key]').forEach((btn) => {
      btn.addEventListener('click', () => openPodcastFeed(btn.dataset.feedKey));
    });
    updatePodcastSidebarUI();
    if (focusedKey) {
      const target = [...list.querySelectorAll('[data-feed-key]')].find(btn => btn.dataset.feedKey === focusedKey);
      (target || document.getElementById('podcast-all-episodes'))?.focus();
    }
  } catch (err) {
    if (revision !== sidebarRefreshRevision || generation !== podcastSyncStatus().generation) return;
    if (background && list.dataset.syncFingerprint !== undefined) return;
    delete list.dataset.syncFingerprint;
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
    <button id="podcast-all-episodes" type="button" class="btn btn-ghost" aria-label="All podcast episodes" aria-keyshortcuts="o" title="All podcast episodes (o)">All podcast episodes</button>
    <button id="podcast-explore" type="button" class="btn btn-ghost">Explore podcasts</button>
    <div id="podcast-nav-list" class="podcast-nav-list" aria-label="Subscribed podcasts"></div>`;
  const setList = document.getElementById('set-list');
  if (setList) sidebar.insertBefore(section, setList);
  else sidebar.appendChild(section);
  document.getElementById('podcast-all-episodes')?.addEventListener('click', openAllPodcastEpisodes);
  document.getElementById('podcast-explore')?.addEventListener('click', () => openPodcastDiscovery({
    subscribe: async (url, title) => {
      const key = podcastFeedKey(url);
      const result = await setSubscribed({ feed_key: key, feed_url: url, title }, true);
      if (!result) throw new Error('Account changed. Reopen Explore podcasts.');
      return { feedKey: (result.resolved || []).find(row => row.kind === 'feed' && row.from === key)?.to || key };
    },
    refreshFeed: subscription => API.refreshPodcastFeed(subscription.feedKey),
    onSubscribed: () => {
      document.dispatchEvent(new Event('podcast:subscriptions-changed'));
      if (isPodcastVirtualView()) loadMediaCallback();
    },
  }));
}

export async function renderPodcastVirtualView(grid, { revision, alive, pageSize: size } = {}) {
  if (!grid) return null;
  setPodcastViewPageSize(size);
  breadcrumbHidden(true);
  if (state.podcastOverview) {
    const generation = podcastSyncStatus().generation;
    const feeds = await listAccountPodcastSubscriptions(await API.podcasts());
    if ((alive && !alive()) || generation !== podcastSyncStatus().generation) return null;
    grid.innerHTML = filterBarHtml('Podcasts', 'all') + feeds.map(feed =>
      `<div class="media-card podcast-feed-card" data-podcast-feed-key="${escapeHtml(feed.feed_key)}"><button type="button" class="podcast-feed-open" aria-label="Podcast ${escapeHtml(feed.title)}"><div class="thumb-wrap">${episodeArtworkHTML({feed_image_url:feed.image_url})}</div><div class="meta"><div class="title">${escapeHtml(feed.title)}</div></div></button></div>`
    ).join('') + (feeds.length ? '' : '<p>No subscribed podcasts yet. Use Explore podcasts to subscribe.</p>');
    grid.querySelectorAll('[data-podcast-feed-key]').forEach(card => card.querySelector('button').onclick = () => openPodcastFeed(card.dataset.podcastFeedKey));
    bindFilterBar(grid);
    bindPodcastMetadata(grid);
    clearSelection();
    return {page:0,total:feeds.length,hasPrev:false,hasNext:false};
  }
  const filter = state.podcastFeedKey ? (state.podcastFilter || 'uncompleted') : 'all';
  const generation = podcastSyncStatus().generation;
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
  if (podcastSyncStatus().generation !== generation) return null;
  if (revision !== undefined && typeof alive === 'function' && !alive()) return null;

  const hasNext = episodes.length >= pageSize;
  // Pending deletions remain hidden even when a stale server listing arrives.
  episodes = episodes.filter(ep => !episodeSnapshot(ep).deletion?.deleted);
  episodes.sort((a, b) => (Date.parse(b.published_at) || 0) - (Date.parse(a.published_at) || 0));
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
    + (episodes.length ? '' : `<p class="text-muted text-sm grid-full podcast-empty">${feedKey ? 'No episodes match this filter, or the feed has not refreshed yet. Use Refresh feed to fetch its latest episodes.' : 'No episodes match this filter.'}</p>`)
    + (feedKey ? '<button type="button" class="btn grid-full" data-reload-podcast>Refresh feed</button><p class="text-muted text-sm grid-full" role="status" data-podcast-refresh-status></p>' : '')
    + pagerHtml(pageInfo, 'bottom');

  // Align state.media with episode card indices so Play / Space / Enter can
  // resolve downloaded copies the same way set-browse playMediaById does.
  setMedia(mediaSlotsForEpisodes(episodes));

  const before = grid.querySelector('[data-page-pager="bottom"]');
  visibleEpisodes = [];
  episodes.forEach((ep, index) => {
    ep.podcast_generation = podcastSyncStatus().generation;
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
    visibleEpisodes.push({ card, ep });
    if (before) grid.insertBefore(card, before);
    else grid.appendChild(card);
  });

  grid.querySelector('[data-reload-podcast]')?.addEventListener('click', async event => {
    const button = event.currentTarget;
    const status = grid.querySelector('[data-podcast-refresh-status]');
    button.disabled = true; status.textContent = 'Fetching latest episodes…';
    try {
      const result = await API.refreshPodcastFeed(feedKey);
      if (podcastSyncStatus().generation !== generation || state.podcastFeedKey !== feedKey) return;
      state.podcastFeedKey = result?.feed_key || feedKey;
      void refreshPodcastSidebar({ background: true });
      loadMediaCallback();
    } catch (error) {
      if (!button.isConnected || podcastSyncStatus().generation !== generation) return;
      status.textContent = error.message || 'Could not refresh this feed. Retry.';
      button.disabled = false;
    }
  });
  bindFilterBar(grid);
  bindPager(grid, pageInfo);
  clearSelection();
  updatePodcastSidebarUI();
  updatePendingHint();
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
    <div role="group" aria-label="Podcast view"><button type="button" class="btn btn-ghost" data-podcast-view="episodes" aria-pressed="${!state.podcastOverview}">Episodes</button><button type="button" class="btn btn-ghost" data-podcast-view="podcasts" aria-pressed="${!!state.podcastOverview}">Podcasts</button></div>
    ${!state.podcastFeedKey && state.sets.some(set => set.is_podcast) ? `<a class="btn btn-ghost" href="/?set=${state.sets.find(set => set.is_podcast).id}">Downloaded server files</a>` : ''}
    ${state.podcastFeedKey ? `<div class="podcast-filter-bar" role="group" aria-label="Completion filter">${buttons}</div>` : '<p class="text-muted text-sm">Newest first · Listened episodes stay in this list.</p>'}
    <p id="podcast-pending-hint" class="text-muted text-sm" role="status"></p>
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
  grid.querySelectorAll('[data-podcast-view]').forEach(button => button.onclick = () => {
    if (button.dataset.podcastView === 'podcasts') openPodcastOverview();
    else openAllPodcastEpisodes();
  });
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

const downloadControllers = new WeakMap();

function renderViewEpisodeCard(card, ep, { showFeed } = {}) {
  const focusedAction = card.contains(document.activeElement) ? document.activeElement?.dataset.action : null;
  const downloading = Boolean(downloadControllers.get(card));
  card.classList.toggle('listened', Boolean(ep.completed));
  card.innerHTML = episodeCardHtml(ep, { showFeed, downloading });
  bindPodcastMetadata(card);
  const playBtn = card.querySelector('[data-action="play"]');
  const downloadBtn = card.querySelector('[data-action="download-episode"]');
  const cancelBtn = card.querySelector('[data-action="cancel-download"]');
  const completeBtn = card.querySelector('[data-action="complete"]');
  const beginningBtn = card.querySelector('[data-action="play-beginning"]');
  const deleteBtn = card.querySelector('[data-action="delete-download"]');
  const deleteEpisodeBtn = card.querySelector('[data-action="delete-episode"]');
  const moreBtn = card.querySelector('[data-action="more"]');

  playBtn?.addEventListener('click', (e) => {
    e.stopPropagation();
    playEpisode(card, ep);
  });
  beginningBtn?.addEventListener('click', (e) => {
    e.stopPropagation();
    playEpisode(card, ep, true);
  });
  deleteBtn?.addEventListener('click', async (e) => {
    e.stopPropagation();
    await deleteEpisodeDownload(card, ep, deleteBtn);
  });
  deleteEpisodeBtn?.addEventListener('click', async e => {
    e.stopPropagation();
    await deleteEpisode(card, ep, deleteEpisodeBtn);
  });
  downloadBtn?.addEventListener('click', (e) => {
    e.stopPropagation();
    downloadEpisodeOnly(card, ep);
  });
  cancelBtn?.addEventListener('click', (e) => {
    e.stopPropagation();
    downloadControllers.get(card)?.abort();
  });
  completeBtn?.addEventListener('click', async (e) => {
    e.stopPropagation();
    await markCompletion(card, ep, !ep.completed);
  });
  moreBtn?.addEventListener('click', (e) => {
    e.stopPropagation();
    document.dispatchEvent(new CustomEvent('media:actions', { detail: card }));
  });
  if (focusedAction) {
    (card.querySelector(`[data-action="${focusedAction}"]`) || cancelBtn || playBtn || downloadBtn)?.focus();
  }
}

// Compact card HTML: title, status badge, Play/Download (+ Cancel while
// transferring), More for completion/restart/delete; description expands.
// More stays available during transfer so completion remains reachable;
// Cancel is the primary control. Progress shows without a server copy
// (Android 1t3 parity).
export function episodeCardHtml(ep, { showFeed, downloading = false } = {}) {
  const completed = Boolean(ep.completed);
  const downloaded = Boolean(ep.server_copy?.media_id);
  const dateStr = publicationDate(ep.published_at);
  const duration = ep.duration_seconds ? fmtDur(ep.duration_seconds) : '';
  const hasProgress = Number(ep.position_seconds) > 0;
  const canResume = downloaded && hasProgress;
  const position = hasProgress
    ? (canResume ? `Resume ${fmtDur(ep.position_seconds)}` : `In progress · ${fmtDur(ep.position_seconds)}`)
    : '';
  const downloadState = downloading
    ? 'Downloading…'
    : downloaded ? 'Downloaded on server' : 'Not downloaded';
  const completeLabel = completed ? 'Mark uncompleted' : 'Mark completed';
  const feedLine = showFeed && ep.feed_title
    ? `<div class="subtitle">${escapeHtml(ep.feed_title)}</div>`
    : '';
  const metaBits = [dateStr, duration, completed ? 'Listened' : '', position, downloadState].filter(Boolean).join(' · ');
  const playLabel = !downloaded ? 'Download and play' : canResume ? 'Resume' : 'Play';
  const descriptionBlock = showNotesHTML(ep.description);
  const primary = downloading
    ? '<button type="button" class="icon-btn btn-sm" data-action="cancel-download" title="Cancel download" aria-label="Cancel download">✕</button>'
    : `${downloaded ? '' : '<button type="button" class="icon-btn btn-sm btn-download-episode" data-action="download-episode" title="Download to server" aria-label="Download episode to server">↓</button>'}
        <button type="button" class="icon-btn btn-sm" data-action="play" title="${playLabel}" aria-label="${playLabel} episode">▶</button>`;
  const extras = `
        <button type="button" class="icon-btn btn-sm card-action-extra btn-complete${completed ? ' active' : ''}" data-action="complete"
          title="${completeLabel}" aria-label="${completeLabel}" aria-pressed="${completed}">✓</button>
        ${downloaded ? '<button type="button" class="icon-btn btn-sm card-action-extra" data-action="play-beginning" title="Play from beginning" aria-label="Play from beginning">↺</button>' : ''}
        ${downloaded ? '<button type="button" class="icon-btn btn-sm card-action-extra" data-action="delete-download" title="Delete downloaded file" aria-label="Delete downloaded file">🗑</button>' : ''}
        <button type="button" class="icon-btn btn-sm card-action-extra" data-action="delete-episode" title="Delete episode" aria-label="Delete episode">🗑</button>`;
  return `
    <div class="thumb-wrap">
      ${episodeArtworkHTML(ep)}
      <span class="badge">${escapeHtml(metaBits)}</span>
      <div class="card-actions">
        ${primary}
        <button type="button" class="icon-btn btn-sm card-more" data-action="more" title="More episode actions" aria-label="More episode actions" aria-haspopup="menu">☰</button>
        ${extras}
      </div>
    </div>
    <div class="meta">
      <div class="title" title="${escapeHtml(ep.title || 'Untitled')}">${escapeHtml(ep.title || 'Untitled')}</div>
      ${feedLine}
      ${descriptionBlock}
    </div>`;
}

async function runEpisodeDownload(card, ep, { play = false, fromBeginning = false } = {}) {
  if (downloadControllers.get(card)) return;
  const index = parseInt(card.dataset.index, 10);
  const controller = new AbortController();
  downloadControllers.set(card, controller);
  renderViewEpisodeCard(card, ep, { showFeed: !state.podcastFeedKey });
  if (play) toast('Downloading episode…', 'info');
  try {
    const media = await API.downloadEpisode(ep.id, { signal: controller.signal });
    ep.server_copy = { media_id: media.id };
    if (episodeSnapshot(ep).deletion?.deleted) return;
    rememberEpisodeMedia(index, episodePlayableMedia(ep, media));
    card.dataset.id = String(media.id);
    toast('Episode downloaded');
    downloadControllers.delete(card);
    renderViewEpisodeCard(card, ep, { showFeed: !state.podcastFeedKey });
    if (play && !(await playMediaById(media.id, { fromBeginning }))) {
      toast('Episode downloaded; select it to play', 'info');
    }
  } catch (err) {
    downloadControllers.delete(card);
    renderViewEpisodeCard(card, ep, { showFeed: !state.podcastFeedKey });
    if (err?.name === 'AbortError' || controller.signal.aborted) {
      toast('Download canceled', 'info');
      return;
    }
    toast(err.message || 'Download failed', 'error');
  }
}

async function playEpisode(card, ep, fromBeginning = false) {
  const index = parseInt(card.dataset.index, 10);
  let media = Number.isInteger(index) ? state.media[index] : null;
  if (media?.id) {
    card.dataset.id = String(media.id);
    if (!(await playMediaById(media.id, { fromBeginning }))) toast('Select the episode to play', 'info');
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
    if (!(await playMediaById(media.id, { fromBeginning }))) toast('Select the episode to play', 'info');
    return;
  }
  await runEpisodeDownload(card, ep, { play: true, fromBeginning });
}

async function downloadEpisodeOnly(card, ep) {
  await runEpisodeDownload(card, ep, { play: false });
}

async function markCompletion(card, ep, completed) {
  const btn = card.querySelector('[data-action="complete"]');
  if (btn) btn.disabled = true;
  try {
    const result = await setEpisodeCompleted(ep, completed);
    ep.completed = result?.completion?.completed ?? ep.completed;
    toast(ep.completed === completed ? (completed ? 'Marked completed' : 'Marked uncompleted') : 'A newer completion change was kept');
    const filter = state.podcastFeedKey ? (state.podcastFilter || 'uncompleted') : 'all';
    const leaves = (filter === 'uncompleted' && ep.completed) || (filter === 'completed' && !ep.completed);
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

async function deleteEpisode(card, ep, button) {
  if (!confirm(`Delete "${ep.title || 'this episode'}" from your podcast lists on all your devices? It stays deleted after feed refresh and sync. Listening history and downloaded files stay saved. Removing a downloaded file is a separate action.`)) return;
  button.disabled = true;
  try {
    // The state layer persists the edit before starting its network request.
    const request = setEpisodeDeleted(ep);
    if (episodeSnapshot(ep).deletion?.deleted) {
      downloadControllers.get(card)?.abort();
      focusAfterRemoval(card); clearEpisodeSlot(card); card.remove();
    }
    await request;
    toast('Episode deleted from your podcast lists');
  } catch (error) {
    if (episodeSnapshot(ep).deletion?.deleted) {
      toast('Episode deleted on this browser · sync pending. Use Retry to sync it to your other devices.', 'info');
    } else {
      button.disabled = false;
      toast(error.message || 'Delete failed. Try again.', 'error');
    }
  }
}

async function deleteEpisodeDownload(card, ep, button) {
  const id = ep.server_copy?.media_id;
  if (!id || !confirm(`Delete the downloaded file for "${ep.title || 'this episode'}" from the shared server? Other server listeners lose access to this copy. It moves to Trash for seven days, then its file and everyone's media-scoped notes, bookmarks, favorites and shares are removed. Episode history and subscriptions stay, as do copies on Android devices. Owners and admins can restore it from Trash before expiry.`)) return;
  button.disabled = true;
  try {
    try { await API.deleteMedia(id); }
    catch (error) {
      // A lost acknowledgement can leave the copy already in Trash. Read its
      // availability before deciding whether the destructive request failed.
      try { await API.mediaDetail(id); } catch (readError) {
        if (readError.status === 404) { error = null; }
      }
      if (error) throw error;
    }
    ep.server_copy = null;
    delete card.dataset.id;
    const index = Number(card.dataset.index);
    if (Number.isInteger(index)) { const list = state.media.slice(); list[index] = null; setMedia(list); }
    renderViewEpisodeCard(card, ep, { showFeed: !state.podcastFeedKey });
    toast('Server copy is no longer available in podcast views');
  } catch (err) {
    button.disabled = false;
    button.focus();
    toast(err.message || 'Delete failed. Try again.', 'error');
  }
}

function updateVisibleEpisodeState() {
  if (!isPodcastVirtualView()) return;
  for (const { card, ep } of visibleEpisodes) {
    if (!card.isConnected) continue;
    if (ep.podcast_generation !== podcastSyncStatus().generation) { card.remove(); continue; }
    const snapshot = episodeSnapshot(ep);
    const completed = snapshot.completion?.completed ?? ep.completed;
    ep.position_seconds = snapshot.position?.seconds ?? ep.position_seconds;
    ep.completed = completed;
    const filter = state.podcastFeedKey ? (state.podcastFilter || 'uncompleted') : 'all';
    if (snapshot.deletion?.deleted || (filter === 'uncompleted' && completed) || (filter === 'completed' && !completed)) {
      if (card.contains(document.activeElement) || card.classList.contains('selected')) focusAfterRemoval(card);
      clearEpisodeSlot(card);
      card.remove();
    } else renderViewEpisodeCard(card, ep, { showFeed: !state.podcastFeedKey });
  }
  const grid = document.getElementById('media-grid');
  if (grid && !grid.querySelector('.episode-card') && !grid.querySelector('.podcast-empty')) {
    const empty = document.createElement('p');
    empty.className = 'text-muted text-sm grid-full podcast-empty';
    empty.textContent = 'No episodes match this filter.';
    grid.appendChild(empty);
  }
  updatePendingHint();
}

function clearEpisodeSlot(card) {
  const index = Number(card.dataset.index);
  if (Number.isInteger(index)) { const list = state.media.slice(); list[index] = null; setMedia(list); }
}

function updatePendingHint() {
  const hint = document.getElementById('podcast-pending-hint');
  if (!hint) return;
  const status = podcastSyncStatus();
  hint.textContent = status.lastError ? (status.pending ? 'Podcast sync could not finish · changes pending. Episode deletions are saved on this browser. ' : 'Podcast sync could not finish. ') : status.pending ? 'Saving podcast changes… ' : '';
  if (!status.lastError) return;
  const retry = document.createElement('button');
  retry.type = 'button'; retry.className = 'btn btn-sm'; retry.textContent = 'Retry';
  retry.addEventListener('click', () => { void retryPodcastEpisodeEdits().catch(() => {}); });
  hint.appendChild(retry);
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
