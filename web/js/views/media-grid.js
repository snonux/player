import { API } from '../api.js';
import { state, setMedia } from '../state.js';
import { clearSelection, selectByElement } from '../selection.js';
import { fmtSize } from '../dom.js';
import { escapeHtml, fmtDur, toast } from '../utils.js';

let callbacks = {};
let mediaPageSize = 100;
let lastMediaPageKey = '';

export function setMediaPageSize(value) {
  const parsed = Number.parseInt(value, 10);
  mediaPageSize = Number.isFinite(parsed) && parsed > 0 ? parsed : 100;
  state.mediaPage = 0;
}

export function paginateItems(items, page, pageSize = mediaPageSize) {
  const size = Math.max(1, Number.parseInt(pageSize, 10) || 100);
  const total = items.length;
  const maxPage = Math.max(0, Math.ceil(total / size) - 1);
  const current = Math.max(0, Math.min(Number.parseInt(page, 10) || 0, maxPage));
  const start = current * size;
  const end = Math.min(start + size, total);
  return {
    items: items.slice(start, end),
    total,
    page: current,
    pageSize: size,
    start,
    end,
    hasPrev: current > 0,
    hasNext: current < maxPage,
  };
}

export function initMediaGrid(options = {}) {
  callbacks = options;
  const grid = document.getElementById('media-grid');

  grid?.addEventListener('dblclick', (e) => {
    const el = e.target.closest('.media-card, .media-row');
    if (!el) return;
    selectByElement(el);
    if (el.classList.contains('set-card')) {
      callbacks.openSet?.(parseInt(el.dataset.setId, 10));
      return;
    }
    callbacks.playSelected?.();
  });

  grid?.addEventListener('click', (e) => {
    const folder = e.target.closest('.folder-card');
    if (folder) {
      e.stopPropagation();
      enterFolder(folder.dataset.name);
    }
  });

  grid?.addEventListener('podcast:episode-downloaded', () => {
    loadMedia();
  });
}

export async function loadMedia() {
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

    if (!singleSetId && !setIds && !callbacks.isShuffle?.() && !hasActiveFilters()) {
      breadcrumb?.classList.add('hidden');
      setMedia([]);
      syncMediaPage('sets');
      const page = renderSetGrid(state.sets || []);
      resultCount.textContent = resultText(state.sets.length, page);
    } else if (singleSetId && !setIds && !callbacks.isShuffle?.() && !hasActiveFilters()) {
      syncMediaPage(`browse:${singleSetId}:${state.folderPath || ''}`);
      const data = await API.browse(singleSetId, state.folderPath);
      updateBreadcrumb(data.current_path);
      setMedia(mediaWithBrowsePath(data.media || [], data.current_path || ''));
      const page = renderBrowse(data);
      const total = (data.media?.length || 0) + (data.folders?.length || 0) + (data.episodes?.length || 0);
      resultCount.textContent = resultText(total, page);
    } else {
      breadcrumb?.classList.add('hidden');
      const sort = callbacks.isShuffle?.() ? 'random' : (state.filters.sort || 'name');
      const params = {
        set_id: setIds ? '' : String(singleSetId || state.selectedSetId || ''),
        set_ids: setIds,
        type: state.filters.type,
        search: state.filters.search,
        favorites: state.filters.favorites ? 'true' : '',
        tags: state.filters.tags || '',
        min_duration: state.filters.minDuration ? String(parseFloat(state.filters.minDuration) * 60) : '',
        max_duration: state.filters.maxDuration ? String(parseFloat(state.filters.maxDuration) * 60) : '',
        filesize_min: state.filters.minFileSize ? String(parseInt(state.filters.minFileSize, 10) * 1024 * 1024) : '',
        filesize_max: state.filters.maxFileSize ? String(parseInt(state.filters.maxFileSize, 10) * 1024 * 1024) : '',
        sort,
        limit: '1000',
      };
      syncMediaPage(`grid:${JSON.stringify(params)}`);
      const data = await API.media(params);
      const list = Array.isArray(data) ? data : data?.media || [];
      setMedia(list);
      const page = renderGrid(list);
      resultCount.textContent = resultText(list.length, page);
    }
  } catch (err) {
    grid.innerHTML = `<p class="error-message grid-full">${escapeHtml(err.message)}</p>`;
  }
}

function syncMediaPage(key) {
  if (key === lastMediaPageKey) return;
  lastMediaPageKey = key;
  state.mediaPage = 0;
}

function resultText(total, page) {
  if (!page || total <= page.pageSize) return `${total} items`;
  return `${page.start + 1}-${page.end} of ${total} items`;
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
    html += ` <span class="breadcrumb-sep">/</span> <button class="breadcrumb-part" data-folder="${escapeHtml(accumulated)}">${escapeHtml(part)}</button>`;
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

export function enterFolder(name) {
  state.folderPath = state.folderPath ? `${state.folderPath}/${name}` : name;
  loadMedia();
}

export function mediaWithBrowsePath(media, path) {
  const prefix = path ? `${path}/` : '';
  return media.map((m) => {
    const relPath = m.rel_path || '';
    const relativeToCurrent = prefix && relPath.startsWith(prefix)
      ? relPath.slice(prefix.length)
      : relPath;
    return {
      ...m,
      browse_path: path || '',
      flattened_folder: relativeToCurrent.includes('/'),
    };
  });
}

export function navigateBack() {
  if (!state.folderPath) {
    if (state.selectedSetId || state.selectedSetIds.length) {
      state.selectedSetId = null;
      state.selectedSetIds = [];
      state.mediaPage = 0;
      callbacks.onSetCleared?.();
      loadMedia();
    }
    return;
  }
  const last = state.folderPath.lastIndexOf('/');
  if (last < 0) {
    state.folderPath = '';
  } else {
    state.folderPath = state.folderPath.substring(0, last);
  }
  loadMedia();
}

function renderSetGrid(sets) {
  const grid = document.getElementById('media-grid');
  if (!grid) return;
  if (!sets.length) {
    const page = paginateItems([], state.mediaPage);
    state.mediaPage = page.page;
    grid.innerHTML = '<p class="text-muted text-sm grid-full">No sets.</p>';
    clearSelection();
    return page;
  }
  const entries = sets.map((item, index) => ({ item, index }));
  const page = paginateItems(entries, state.mediaPage);
  state.mediaPage = page.page;
  grid.innerHTML = renderPager(page, 'top') +
    page.items.map((entry) => renderSetCard(entry.item, entry.index)).join('') +
    renderPager(page, 'bottom');
  clearSelection();
  bindSetCards(grid);
  bindPager(grid);
  return page;
}

function renderSetCard(set, index) {
  return `
    <div class="media-card set-card" data-set-id="${set.id}" data-index="${index}" tabindex="0" role="button" aria-label="Set ${escapeHtml(set.name)}">
      <div class="thumb-wrap">
        <img src="/api/sets/${set.id}/cover" alt="" loading="lazy" onerror="this.remove();">
        <span class="placeholder">▦</span>
        <div class="card-actions">
          <button class="icon-btn btn-sm" data-action="regen-set-cover" title="Regenerate cover">🔄</button>
        </div>
      </div>
      <div class="meta">
        <div class="title">${escapeHtml(set.name)}</div>
        <div class="subtitle">${set.is_podcast ? 'Podcast set' : 'Set'}</div>
      </div>
    </div>
  `;
}

function bindSetCards(grid) {
  grid.querySelectorAll('.set-card').forEach((el) => {
    el.addEventListener('click', (e) => {
      selectByElement(el);
      if (e.target.closest('.card-actions, button')) return;
      e.stopPropagation();
      callbacks.openSet?.(parseInt(el.dataset.setId, 10));
    });
    el.querySelector('[data-action="regen-set-cover"]')?.addEventListener('click', async (e) => {
      e.stopPropagation();
      const id = parseInt(el.dataset.setId, 10);
      if (!Number.isFinite(id)) return;
      try {
        await API.regenCover(id);
        toast('Set cover regenerated');
        await loadMedia();
      } catch (err) {
        toast(err.message || 'Cover failed', 'error');
      }
    });
  });
}

function renderBrowse(data) {
  const grid = document.getElementById('media-grid');
  if (!grid) return;
  const folders = data.folders || [];
  const media = mediaWithBrowsePath(data.media || [], data.current_path || '');
  const episodes = data.episodes || [];
  if (!folders.length && !media.length && !episodes.length) {
    const page = paginateItems([], state.mediaPage);
    state.mediaPage = page.page;
    grid.innerHTML = '<p class="text-muted text-sm grid-full">Folder is empty.</p>';
    clearSelection();
    return page;
  }
  const entries = [
    ...folders.map((folder, index) => ({ type: 'folder', item: folder, index })),
    ...media.map((item, index) => ({ type: 'media', item, index })),
    ...episodes.map((item, index) => ({ type: 'episode', item, index })),
  ];
  const page = paginateItems(entries, state.mediaPage);
  state.mediaPage = page.page;
  const folderHtml = page.items
    .filter((entry) => entry.type === 'folder')
    .map((entry) => renderFolder(entry.item, entry.index))
    .join('');
  const mediaHtml = page.items
    .filter((entry) => entry.type === 'media')
    .map((entry) => renderItem(entry.item, entry.index))
    .join('');
  const pageEpisodes = page.items
    .filter((entry) => entry.type === 'episode')
    .map((entry) => entry.item);
  grid.innerHTML = renderPager(page, 'top') + folderHtml + mediaHtml + renderPager(page, 'bottom');
  clearSelection();

  grid.querySelectorAll('.folder-card [data-action="regen-folder-cover"]').forEach((b) => {
    b.addEventListener('click', async (e) => {
      e.stopPropagation();
      const setId = state.selectedSetId;
      if (!setId) return;
      const name = b.closest('.folder-card')?.dataset.name;
      const folder = state.folderPath ? `${state.folderPath}/${name}` : name;
      try {
        await API.regenCover(setId, folder);
        toast('Folder cover regenerated');
        await loadMedia();
        const folderCard = document.querySelector(`.folder-card[data-name="${CSS.escape(name)}"]`);
        const img = folderCard?.querySelector('img');
        if (img) {
          const base = img.src.split('?')[0];
          img.src = `${base}?t=${Date.now()}`;
        }
      } catch (err) {
        toast(err.message || 'Cover failed', 'error');
      }
    });
  });

  bindMediaItems(grid);
  bindPager(grid);

  if (pageEpisodes.length) {
    import('../podcasts.js').then(m => {
      m.renderPodcastEpisodes(grid, pageEpisodes, {
        before: grid.querySelector('[data-page-pager="bottom"]'),
      });
    }).catch(() => {});
  }
  return page;
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
  if (!items.length) {
    const page = paginateItems([], state.mediaPage);
    state.mediaPage = page.page;
    grid.innerHTML = `<p class="text-muted text-sm grid-full">No results.</p>`;
    clearSelection();
    return page;
  }
  const entries = items.map((item, index) => ({ item, index }));
  const page = paginateItems(entries, state.mediaPage);
  state.mediaPage = page.page;
  grid.innerHTML = renderPager(page, 'top') +
    page.items.map((entry) => renderItem(entry.item, entry.index)).join('') +
    renderPager(page, 'bottom');
  clearSelection();
  bindMediaItems(grid);
  bindPager(grid);
  return page;
}

function renderPager(page, position) {
  if (!page || page.total <= page.pageSize) return '';
  const showPrev = position === 'top' && page.hasPrev;
  const showNext = position === 'bottom' && page.hasNext;
  if (!showPrev && !showNext) return '';
  const action = showPrev ? 'prev' : 'next';
  const label = showPrev ? 'Prev' : 'Next';
  return `
    <div class="media-pager media-pager-${position} grid-full" data-page-pager="${position}">
      <span class="media-page-summary">${page.start + 1}-${page.end} of ${page.total}</span>
      <button class="btn btn-ghost btn-sm" type="button" data-page-action="${action}">${label}</button>
    </div>
  `;
}

function bindPager(grid) {
  grid.querySelectorAll('[data-page-action]').forEach((button) => {
    button.addEventListener('click', () => {
      state.mediaPage += button.dataset.pageAction === 'next' ? 1 : -1;
      loadMedia();
    });
  });
}

function bindMediaItems(grid) {
  grid.querySelectorAll('.media-card, .media-row').forEach((el) => {
    el.addEventListener('click', (e) => {
      selectByElement(el);
      if (el.dataset.flattenedFolder !== 'true' || e.target.closest('.card-actions, button')) return;
      callbacks.playSelected?.();
    });
    const playBtn = el.querySelector('[data-action="play"]');
    const viewBtn = el.querySelector('[data-action="view"]');
    const favBtn = el.querySelector('[data-action="favorite"]');
    const noteBtn = el.querySelector('[data-action="notes"]');
    const downloadBtn = el.querySelector('[data-action="download"]');
    const tagBtn = el.querySelector('[data-action="tags"]');
    const thumbBtn = el.querySelector('[data-action="regen-thumb"]');
    playBtn?.addEventListener('click', (e) => {
      e.stopPropagation();
      selectByElement(el);
      callbacks.playSelected?.();
    });
    viewBtn?.addEventListener('click', (e) => {
      e.stopPropagation();
      selectByElement(el);
      callbacks.playSelected?.();
    });
    favBtn?.addEventListener('click', (e) => {
      e.stopPropagation();
      callbacks.toggleFavorite?.(el.dataset.id, favBtn);
    });
    noteBtn?.addEventListener('click', (e) => {
      e.stopPropagation();
      callbacks.openNotesForSelected?.();
    });
    downloadBtn?.addEventListener('click', (e) => {
      e.stopPropagation();
      window.open(`/api/media/${el.dataset.id}/download`, '_blank');
    });
    tagBtn?.addEventListener('click', (e) => {
      e.stopPropagation();
      callbacks.openTagsForElement?.(el);
    });
    thumbBtn?.addEventListener('click', (e) => {
      e.stopPropagation();
      callbacks.regenThumb?.(el.dataset.id);
    });
  });
}

function renderItem(m, index) {
  const sizeText = fmtSize(m.file_size_bytes);
  const flattenedFolder = m.flattened_folder ? 'true' : 'false';
  if (m.type === 'video') {
    return `
      <div class="media-card" data-id="${m.id}" data-index="${index}" data-flattened-folder="${flattenedFolder}" tabindex="0" role="button" aria-label="${escapeHtml(m.file_name)}">
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
          <div class="subtitle">${escapeHtml(m.codec || '')} ${escapeHtml(m.resolution || '')} ${m.bitrate ? Math.round(m.bitrate / 1000) + 'kbps' : ''}</div>
        </div>
      </div>
    `;
  }
  if (m.type === 'image') {
    const resText = escapeHtml(m.resolution || '');
    const safeSizeText = escapeHtml(sizeText || '');
    return `
      <div class="media-card image-card" data-id="${m.id}" data-index="${index}" data-flattened-folder="${flattenedFolder}" tabindex="0" role="button" aria-label="${escapeHtml(m.file_name)}">
        <div class="thumb-wrap">
          ${m.thumbnail_path ? `<img src="/api/media/${m.id}/thumbnail" alt="" loading="lazy">` : `<span class="placeholder">No image</span>`}
          <span class="badge">${resText}${safeSizeText ? ' • ' + safeSizeText : ''}</span>
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
          <div class="subtitle">${resText} ${safeSizeText}</div>
        </div>
      </div>
    `;
  }
  return `
    <div class="media-card audio-card" data-id="${m.id}" data-index="${index}" data-flattened-folder="${flattenedFolder}" tabindex="0" role="button" aria-label="${escapeHtml(m.file_name)}">
      <div class="thumb-wrap">
        ${m.thumbnail_path ? `<img src="/api/media/${m.id}/thumbnail" alt="" loading="lazy">` : `<span class="placeholder">No cover</span>`}
        <span class="badge">${fmtDur(m.duration)}${sizeText ? ' • ' + sizeText : ''}</span>
        <div class="card-actions">
          <button class="icon-btn btn-sm" data-action="play" title="Play">▶</button>
          <button class="icon-btn btn-sm" data-action="favorite" title="Favorite">♥</button>
          <button class="icon-btn btn-sm" data-action="notes" title="Notes">📝</button>
          <button class="icon-btn btn-sm" data-action="download" title="Download">⬇</button>
          <button class="icon-btn btn-sm" data-action="tags" title="Tags">🏷</button>
        </div>
      </div>
      <div class="meta">
        <div class="title">${escapeHtml(m.file_name)}</div>
        <div class="subtitle">${escapeHtml(m.codec || '')} ${m.bitrate ? Math.round(m.bitrate/1000)+'kbps' : ''}</div>
      </div>
    </div>
  `;
}

export function hasActiveFilters() {
  return !!(
    state.filters.search ||
    state.filters.type ||
    state.filters.favorites ||
    state.filters.tags ||
    state.filters.minDuration ||
    state.filters.maxDuration ||
    state.filters.minFileSize ||
    state.filters.maxFileSize ||
    state.filters.sort
  );
}
