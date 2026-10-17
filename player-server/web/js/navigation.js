import { API } from './api.js';
import { state } from './state.js';
import { selectMedia, currentMediaId, stopAndClose } from './player.js';
import { selectByElement } from './selection.js';
import { loadMedia } from './views/media-grid.js';
import { updateSetRowsUI } from './views/sets.js';
import { readLibraryURL, libraryURL } from './navigation-url.js';

export function initNavigation({ isShuffle, setShuffle, syncChrome }) {
  let restoring = false;
  let revision = 0;
  let mediaID = null;
  const status = document.createElement('div');
  status.className = 'navigation-status hidden';
  status.setAttribute('role', 'alert');
  document.querySelector('.main-content').prepend(status);

  function commit(replace = false) {
    if (restoring) return;
    const url = libraryURL(state, mediaID, isShuffle());
    if (url === location.pathname + location.search) return;
    history[replace ? 'replaceState' : 'pushState'](null, '', url);
  }
  document.addEventListener('library:navigating', event => {
    if (event.detail?.restoring) return;
    revision++;
    restoring = false;
    status.classList.add('hidden');
    commit();
  });
  document.addEventListener('library:navigated', () => commit(true));
  document.addEventListener('player:media-opened', event => {
    mediaID = event.detail.media.id;
    document.title = `${event.detail.media.file_name} — Player`;
    commit();
  });
  document.addEventListener('player:closed', () => { mediaID = null; document.title = 'Player'; commit(); });
  window.addEventListener('popstate', () => restore());

  async function restore() {
    const operation = ++revision;
    const route = readLibraryURL(location.search);
    restoring = true;
    status.classList.add('hidden');
    document.dispatchEvent(new Event('navigation:restore'));
    try {
      // Read the authoritative item even when it isn't in the current page,
      // filter result or folder. Authorization stays with the normal API.
      const detail = route.media ? await API.mediaDetail(route.media) : null;
      if (operation !== revision) return;
      const media = detail?.media;
      if (route.media && !media?.id) throw new Error('Media is unavailable.');
      state.selectedSetIds = route.sets.length ? route.sets : media ? [media.set_id] : [];
      state.selectedSetId = state.selectedSetIds.at(-1) || null;
      state.virtualSet = route.virtual;
      state.folderPath = route.folder;
      if (media && !route.sets.length) {
        const path = media.rel_path || media.file_name || '';
        state.folderPath = path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '';
      }
      Object.assign(state.filters, route.filters);
      setShuffle(route.shuffle);
      updateSetRowsUI();
      syncChrome();
      document.dispatchEvent(new CustomEvent('filters:changed'));
      const input = document.getElementById('search-input');
      if (input) input.value = route.filters.search;
      await loadMedia({ restoring: true, page: route.page, mediaId: route.media });
      if (operation !== revision) return;
      if (media) {
        const index = state.media.findIndex(item => item.id === media.id);
        const card = document.querySelector(`#media-grid [data-id="${media.id}"]`);
        if (card) { selectByElement(card); card.scrollIntoView({ block: 'nearest' }); }
        if (currentMediaId() !== media.id) selectMedia(media, index, detail?.progress?.position_seconds ?? 0);
        document.title = `${media.file_name} — Player`;
      } else {
        if (currentMediaId()) stopAndClose();
        document.title = 'Player';
      }
      mediaID = route.media;
      restoring = false;
      commit(true);
    } catch (error) {
      if (operation !== revision) return;
      if (currentMediaId()) stopAndClose();
      mediaID = route.media;
      const text = document.createElement('p');
      text.textContent = `Could not open this library link: ${error.message}`;
      const retry = document.createElement('button');
      retry.className = 'btn btn-sm';
      retry.textContent = 'Retry link';
      retry.addEventListener('click', restore);
      status.replaceChildren(text, retry);
      status.classList.remove('hidden');
      restoring = false;
    }
  }
  return { restore };
}
