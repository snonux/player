import { podcastStreamMedia } from './podcast-playback.js';
import { podcastDownloading, downloadPodcastEpisode, cancelPodcastDownload } from './podcast-downloads.js';
import { bindPodcastMultiselect } from './podcast-multiselect.js';
import { episodeArtworkHTML, bindPodcastMetadata, episodeCardMarkup, confirmEpisodeDelete } from './podcast-metadata.js';
import { bindSwipeDelete } from './swipe-delete.js';
import { openPodcastDiscovery } from './podcast-discovery.js';
import { setSubscribed, listAccountPodcastSubscriptions } from './podcast-subscriptions.js';
import { podcastFeedKey } from './podcast-identity.js';
// podcast-views.js — subscription folders and All podcast episodes (§6.3).
// Uses feed_key identity, completion filters, and server pagination. Keeps
// downloaded episodes as podcast cards (server_copy) instead of dropping
// them into generic media cards.
import { API } from './api.js';
import { state, setMedia } from './state.js';
import { escapeHtml, toast } from './utils.js';
import { setEpisodeCompleted, setEpisodeDeleted, episodeSnapshot, podcastSyncStatus, retryPodcastEpisodeEdits } from './podcast-episode-state.js';
import { clearSelection, selectByElement, currentElement } from './selection.js';
import { dismissSidebarAfterNav } from './views/help.js';

// episodePlayableMedia builds a state.media slot for playMediaById /
// playSelected. Subscription views clear the browse list, so Play must put
// downloaded copies (or a fresh download) into state.media by card index.
export function episodePlayableMedia(ep, downloaded = null) {
  const podcast = { ...downloaded?.podcast, title: ep.title,
    feed_title: ep.podcast_title || ep.feed_title || downloaded?.podcast?.feed_title || '',
    description: ep.description || downloaded?.podcast?.description || '',
    image_url: ep.image_url || downloaded?.podcast?.image_url || '',
    feed_image_url: ep.feed_image_url || downloaded?.podcast?.feed_image_url || '' };
  if (downloaded?.id) return { ...downloaded, podcast, podcast_episode: episodeSnapshot(ep) };
  const id = ep?.server_copy?.media_id;
  if (!id) return podcastStreamMedia(ep);
  return {
    id,
    type: 'audio',
    title: ep.title || '',
    is_podcast: true,
    podcast,
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
  { id: 'started', label: 'In progress' },
  { id: 'completed', label: 'Completed' },
  { id: 'all', label: 'All' },
];

let loadMediaCallback = () => {};
let playMediaById = async () => false;
let pageSize = 100;
let sidebarFeeds = [];
let visibleEpisodes = [];
let episodeSelection;
let episodeLayout = 'grid';
let stateUpdateTimer;
let sidebarRefreshRevision = 0;
// Episode to start once the In progress view has rendered (resume shortcut).
let pendingResume = null;

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

// Resume shortcut: show In progress, then play the episode from its card so
// it starts exactly like the card's own Play button (resume position, queue
// continuation and podcast player chrome included).
export function resumePodcastEpisode(ep) {
  if (!ep?.feed_key || !ep?.episode_key) return;
  pendingResume = { feed_key: ep.feed_key, episode_key: ep.episode_key };
  state.podcastFilter = 'started';
  openPodcastView('');
}

export function openPodcastsInProgress() {
  state.podcastFilter = 'started';
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
        return `<button type="button" class="btn btn-ghost nav-item podcast-nav-feed" data-feed-key="${escapeHtml(p.feed_key || '')}" aria-label="Podcast ${name}" title="${name}">${name}</button>`;
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
  section.className = 'sidebar-section';
  section.innerHTML = `
    <div class="sidebar-header"><h2>Podcasts</h2></div>
    <button id="podcast-all-episodes" type="button" class="btn btn-ghost nav-item" aria-label="All podcast episodes" aria-keyshortcuts="o" title="All podcast episodes (o)">All podcast episodes</button>
    <button id="podcast-explore" type="button" class="btn btn-ghost nav-item">Explore podcasts</button>
    <div id="podcast-nav-list" class="podcast-nav-list" aria-label="Subscribed podcasts"></div>`;
  // The slot keeps Podcasts between Music and Radio; older markup without it
  // falls back to just before the sets list.
  const slot = document.getElementById('podcast-nav-slot');
  const setList = document.getElementById('set-list');
  if (slot) slot.replaceWith(section);
  else if (setList) setList.parentElement.insertBefore(section, setList);
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
  grid.classList.remove('picture-grid');
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
  const filter = state.podcastFilter || 'uncompleted';
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
  // In progress keeps the server's most recently listened order.
  if (filter !== 'started') episodes.sort((a, b) => (Date.parse(b.published_at) || 0) - (Date.parse(a.published_at) || 0));
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

  grid.classList.toggle('podcast-episode-list', episodeLayout === 'list');
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
    const mediaId = state.media[index]?.id;
    if (mediaId) card.dataset.id = String(mediaId);
    card.tabIndex = -1;
    renderViewEpisodeCard(card, ep, { showFeed: !feedKey });
    // List view only, as on Android; selection mode keeps its own gestures.
    bindSwipeDelete(card, () => card.querySelector('[data-action="delete-episode"]:not(:disabled)')?.click(),
      { enabled: () => grid.classList.contains('podcast-episode-list') && !card.classList.contains('podcast-selecting') });
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
  episodeSelection = bindPodcastMultiselect(grid, visibleEpisodes, {
    current: () => isPodcastVirtualView() && !state.podcastOverview && !state.music && podcastSyncStatus().generation === generation &&
      state.podcastFeedKey === feedKey && state.podcastFilter === filter && state.mediaPage === page,
    downloads: true,
    deleteMessage: (action, count) => action === 'delete'
      ? `Delete ${count} episodes from your podcast lists on all your devices? They stay deleted after feed refresh and sync. Listening history and downloaded files stay saved. Removing downloaded files is a separate action.`
      : `Delete ${count} selected downloaded files from the shared server? Other server listeners lose access to these copies. They move to Trash for seven days, then their files and everyone's media-scoped notes, bookmarks, favorites and shares are removed. Episode history and subscriptions stay, as do copies on Android devices. Owners and admins can restore them from Trash before expiry.`,
    apply: async (ep, action, current) => {
      if (!current()) throw new Error('Account or view changed');
      if (action === 'remove-download') {
        if (ep.server_copy?.media_id) { await API.deleteMedia(ep.server_copy.media_id); if (current()) ep.server_copy = null; }
      } else if (action === 'delete') {
        await setEpisodeDeleted(ep);
        if (current()) {
          const card = visibleEpisodes.find(entry => entry.ep === ep)?.card;
          if (card) { clearEpisodeSlot(card); card.remove(); }
        }
      } else {
        const desired = action === 'completed';
        const result = await setEpisodeCompleted(ep, desired);
        if (result?.completion?.completed !== desired) throw new Error('A newer completion change was kept');
        ep.completed = desired;
      }
      if (current() && action !== 'delete') {
        const card = visibleEpisodes.find(entry => entry.ep === ep)?.card;
        if (card) renderViewEpisodeCard(card, ep, { showFeed: !state.podcastFeedKey });
      }
    },
    notify: toast,
    refresh: () => loadMediaCallback(),
  });
  bindFilterBar(grid);
  bindPager(grid, pageInfo);
  clearSelection();
  updatePodcastSidebarUI();
  updatePendingHint();
  const resume = pendingResume;
  pendingResume = null;
  const target = resume && visibleEpisodes.find(({ ep }) =>
    ep.feed_key === resume.feed_key && ep.episode_key === resume.episode_key);
  if (target) {
    selectByElement(target.card);
    void playEpisode(target.card, target.ep);
  } else if (resume) toast('This episode is no longer in progress', 'info');
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
    ${state.podcastOverview ? '' : `<div class="podcast-filter-bar" role="group" aria-label="Completion filter">${buttons}</div>`}
    ${state.podcastOverview ? '' : `<div class="episode-layout-controls" role="group" aria-label="Episode layout"><button type="button" class="btn" data-episode-layout="list" aria-pressed="${episodeLayout === 'list'}">List</button><button type="button" class="btn" data-episode-layout="grid" aria-pressed="${episodeLayout === 'grid'}">Grid</button></div>`}
    <details><summary>Episode retention</summary><p>Completed episodes leave your lists after seven days. This browser and the server each start that grace when they first observe completion. Playing again or restoring an episode keeps it until you complete it again. Expired browser copies are removed when playback releases them; shared server files and listening history stay saved.</p></details>
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
  grid.querySelectorAll('[data-episode-layout]').forEach(button => button.onclick = () => {
    episodeLayout = button.dataset.episodeLayout;
    grid.classList.toggle('podcast-episode-list', episodeLayout === 'list');
    grid.querySelectorAll('[data-episode-layout]').forEach(control => control.setAttribute('aria-pressed', String(control.dataset.episodeLayout === episodeLayout)));
  });
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

const renderedEpisodes = new WeakMap();
globalThis.document?.addEventListener?.('podcast:download-changed', event => {
  for (const card of document.querySelectorAll('.episode-card[data-episode-id]')) {
    const ep = renderedEpisodes.get(card);
    if (ep && String(ep.id) === String(event.detail.id)) {
      if (event.detail.media) {
        ep.server_copy = { media_id: event.detail.media.id };
        rememberEpisodeMedia(parseInt(card.dataset.index, 10), episodePlayableMedia(ep, event.detail.media));
        card.dataset.id = String(event.detail.media.id);
      }
      renderViewEpisodeCard(card, ep, {showFeed: !state.podcastFeedKey});
    }
  }
});

function renderViewEpisodeCard(card, ep, { showFeed } = {}) {
  const selectionFocused = card.querySelector('.media-selection-check') === document.activeElement;
  const focusedAction = card.contains(document.activeElement) ? document.activeElement?.dataset.action : null;
  renderedEpisodes.set(card, ep);
  const downloading = podcastDownloading(ep.id);
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
    cancelPodcastDownload(ep.id);
  });
  completeBtn?.addEventListener('click', async (e) => {
    e.stopPropagation();
    await markCompletion(card, ep, !ep.completed);
  });
  moreBtn?.addEventListener('click', (e) => {
    e.stopPropagation();
    document.dispatchEvent(new CustomEvent('media:actions', { detail: card }));
  });
  episodeSelection?.refresh();
  if (selectionFocused) card.querySelector('.media-selection-check')?.focus();
  if (focusedAction) {
    (card.querySelector(`[data-action="${focusedAction}"]`) || cancelBtn || playBtn || downloadBtn)?.focus();
  }
}

// Subscription episode card (see episodeCardMarkup).
export function episodeCardHtml(ep, { showFeed, downloading = false } = {}) {
  return episodeCardMarkup(ep, {
    completed: Boolean(ep.completed),
    downloaded: Boolean(ep.server_copy?.media_id),
    downloading,
    feedTitle: showFeed ? ep.feed_title : '',
    downloadLabel: { title: 'Download to server', aria: 'Download episode to server' },
    deletable: true,
    showListened: true,
  });
}

async function runEpisodeDownload(card, ep) {
  if (podcastDownloading(ep.id)) return;
  const lease = podcastSyncStatus().generation;
  try {
    const media = await downloadPodcastEpisode(ep.id);
    if (podcastSyncStatus().generation !== lease) return;
    ep.server_copy = { media_id: media.id };
    if (episodeSnapshot(ep).deletion?.deleted) return;
    if (card.isConnected) {
      rememberEpisodeMedia(parseInt(card.dataset.index, 10), episodePlayableMedia(ep, media));
      card.dataset.id = String(media.id);
      renderViewEpisodeCard(card, ep, { showFeed: !state.podcastFeedKey });
    }
    toast('Episode downloaded');
  } catch (err) {
    if (podcastSyncStatus().generation !== lease) return;
    toast(err?.name === 'AbortError' ? 'Download canceled' : err.message || 'Download failed', err?.name === 'AbortError' ? 'info' : 'error');
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
  const stream = podcastStreamMedia(ep);
  if (!stream) return;
  rememberEpisodeMedia(index, stream);
  card.dataset.id = String(stream.id);
  if (!(await playMediaById(stream.id, { fromBeginning }))) toast('Select the episode to play', 'info');
}

async function downloadEpisodeOnly(card, ep) {
  await runEpisodeDownload(card, ep);
}

async function markCompletion(card, ep, completed) {
  const btn = card.querySelector('[data-action="complete"]');
  if (btn) btn.disabled = true;
  try {
    const result = await setEpisodeCompleted(ep, completed);
    ep.completed = result?.completion?.completed ?? ep.completed;
    toast(ep.completed === completed ? (completed ? 'Marked completed' : 'Marked uncompleted') : 'A newer completion change was kept');
    const filter = state.podcastFilter || 'uncompleted';
    const leaves = (filter !== 'completed' && filter !== 'all' && ep.completed) || (filter === 'completed' && !ep.completed);
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
  if (!confirmEpisodeDelete(ep.title)) return;
  button.disabled = true;
  try {
    // The state layer persists the edit before starting its network request.
    const request = setEpisodeDeleted(ep);
    if (episodeSnapshot(ep).deletion?.deleted) {
      cancelPodcastDownload(ep.id);
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
  if (episodeSelection?.active) {
    if (visibleEpisodes.some(({ep}) => ep.podcast_generation !== podcastSyncStatus().generation)) { loadMediaCallback(); return; }
    updatePendingHint(); return;
  }
  for (const { card, ep } of visibleEpisodes) {
    if (!card.isConnected) continue;
    if (ep.podcast_generation !== podcastSyncStatus().generation) { card.remove(); continue; }
    const snapshot = episodeSnapshot(ep);
    const completed = snapshot.completion?.completed ?? ep.completed;
    ep.position_seconds = snapshot.position?.seconds ?? ep.position_seconds;
    ep.completed = completed;
    const filter = state.podcastFilter || 'uncompleted';
    if (snapshot.deletion?.deleted || (filter !== 'completed' && filter !== 'all' && completed) || (filter === 'completed' && !completed)) {
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
