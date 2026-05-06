import { API } from '../api.js';
import { state, setMedia } from '../state.js';
import { clearSelection, selectByElement } from '../selection.js';
import { fmtSize } from '../dom.js';
import { escapeHtml, fmtDur, toast } from '../utils.js';

let callbacks = {};

export function initMediaGrid(options = {}) {
  callbacks = options;
  const grid = document.getElementById('media-grid');

  grid?.addEventListener('dblclick', (e) => {
    const el = e.target.closest('.media-card, .media-row');
    if (!el) return;
    selectByElement(el);
    const idx = parseInt(el.dataset.index, 10);
    const media = state.media[idx];
    if (media?.type === 'image') {
      callbacks.openLightbox?.(state.media, media.id);
    } else {
      callbacks.playSelected?.();
    }
  });

  grid?.addEventListener('click', (e) => {
    const folder = e.target.closest('.folder-card');
    if (folder) {
      e.stopPropagation();
      enterFolder(folder.dataset.name);
    }
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

    if (singleSetId && !setIds && !callbacks.isShuffle?.() && !hasActiveFilters()) {
      const data = await API.browse(singleSetId, state.folderPath);
      updateBreadcrumb(data.current_path);
      setMedia(mediaWithBrowsePath(data.media || [], data.current_path || ''));
      renderBrowse(data);
      const total = (data.media?.length || 0) + (data.folders?.length || 0);
      resultCount.textContent = `${total} items`;
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
      const data = await API.media(params);
      const list = Array.isArray(data) ? data : data?.media || [];
      setMedia(list);
      renderGrid(list);
      resultCount.textContent = `${list.length} items`;
    }
  } catch (err) {
    grid.innerHTML = `<p class="error-message grid-full">${escapeHtml(err.message)}</p>`;
  }
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
  return media.map((m) => ({ ...m, browse_path: path || '' }));
}

export function navigateBack() {
  if (!state.folderPath) return;
  const last = state.folderPath.lastIndexOf('/');
  if (last < 0) {
    state.folderPath = '';
  } else {
    state.folderPath = state.folderPath.substring(0, last);
  }
  loadMedia();
}

function renderBrowse(data) {
  const grid = document.getElementById('media-grid');
  if (!grid) return;
  const folders = data.folders || [];
  const media = mediaWithBrowsePath(data.media || [], data.current_path || '');
  if (!folders.length && !media.length) {
    grid.innerHTML = '<p class="text-muted text-sm grid-full">Folder is empty.</p>';
    clearSelection();
    return;
  }
  const folderHtml = folders.map((f, i) => renderFolder(f, i)).join('');
  const mediaHtml = media.map((m, i) => renderItem(m, i)).join('');
  grid.innerHTML = folderHtml + mediaHtml;
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

  if (data.episodes && data.episodes.length) {
    import('../podcasts.js').then(m => {
      m.renderPodcastEpisodes(grid, data.episodes);
    }).catch(() => {});
  }
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
    grid.innerHTML = `<p class="text-muted text-sm grid-full">No results.</p>`;
    clearSelection();
    return;
  }
  grid.innerHTML = items.map((m, i) => renderItem(m, i)).join('');
  clearSelection();
  bindMediaItems(grid);
}

function bindMediaItems(grid) {
  grid.querySelectorAll('.media-card, .media-row').forEach((el) => {
    el.addEventListener('click', () => { selectByElement(el); });
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
      const idx = parseInt(el.dataset.index, 10);
      const media = state.media[idx];
      if (media) callbacks.openLightbox?.(state.media, media.id);
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
  if (m.type === 'video') {
    return `
      <div class="media-card" data-id="${m.id}" data-index="${index}" tabindex="0" role="button" aria-label="${escapeHtml(m.file_name)}">
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
      <div class="media-card image-card" data-id="${m.id}" data-index="${index}" tabindex="0" role="button" aria-label="${escapeHtml(m.file_name)}">
        <div class="thumb-wrap">
          ${m.thumbnail_path ? `<img src="/api/media/${m.id}/thumbnail" alt="" loading="lazy">` : `<span class="placeholder">No image</span>`}
          <span class="badge">${resText}${safeSizeText ? ' • ' + safeSizeText : ''}</span>
          <div class="card-actions">
            <button class="icon-btn btn-sm" data-action="view" title="View">👁</button>
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
    <div class="media-card audio-card" data-id="${m.id}" data-index="${index}" tabindex="0" role="button" aria-label="${escapeHtml(m.file_name)}">
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
