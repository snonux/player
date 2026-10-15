import { API } from './api.js';

export function bookmarkTime(seconds) {
  const value = Math.max(0, Math.floor(seconds));
  const h = Math.floor(value / 3600);
  const m = Math.floor(value % 3600 / 60);
  const s = String(value % 60).padStart(2, '0');
  return h ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`;
}

export function bookmarkRows(value, mediaId) {
  if (!Array.isArray(value) || value.some(row => !row ||
      !Number.isSafeInteger(row.id) || row.id <= 0 || row.media_id !== mediaId ||
      !Number.isFinite(row.position_seconds) || row.position_seconds < 0 ||
      typeof row.label !== 'string')) throw new Error('Invalid bookmark response');
  return [...value].sort((a, b) => a.position_seconds - b.position_seconds || a.id - b.id);
}

// Each dialog opening owns its requests. Closing or changing tracks invalidates
// that generation, including a response for an earlier opening of the same item.
export function initBookmarks({ getMedia, getElement, available = () => true, api = API }) {
  const byId = id => document.getElementById(id);
  const modal = byId('bookmarks-modal');
  if (!modal) return null;
  const label = byId('bookmark-label');
  const form = byId('bookmark-form');
  const save = byId('bookmark-save');
  const capture = byId('bookmark-capture');
  const status = byId('bookmarks-status');
  const list = byId('bookmarks-list');
  const retry = byId('bookmarks-retry');
  let epoch = 0;
  let mediaId = null;
  let rows = [];
  let position = null;
  let loading = false;
  let pending = false;
  let loaded = false;
  const valid = token => token === epoch && modal.classList.contains('open') &&
    getMedia()?.id === mediaId && available();
  const close = () => { epoch++; mediaId = null; modal.classList.remove('open'); };
  const message = value => { status.textContent = value; };
  function controls() {
    save.disabled = loading || pending || !loaded || position === null;
    capture.disabled = pending;
    retry.disabled = loading || pending;
    list.querySelectorAll('button').forEach(button => { button.disabled = pending; });
  }
  function capturePosition() {
    const element = getElement();
    position = element?.readyState >= 1 && !element.error && Number.isFinite(element.currentTime)
      ? Math.max(0, element.currentTime) : null;
    const duration = getMedia()?.duration;
    if (position !== null && duration > 0) position = Math.min(position, duration);
    byId('bookmark-position').textContent = position === null
      ? 'Wait for playback to load, then use the current position.'
      : `Position to save: ${bookmarkTime(position)}`;
    controls();
  }
  function render() {
    list.replaceChildren();
    for (const row of rows) {
      const item = document.createElement('li');
      item.className = 'bookmark-row';
      const jump = document.createElement('button');
      jump.type = 'button';
      jump.className = 'btn btn-ghost bookmark-jump';
      jump.textContent = `${bookmarkTime(row.position_seconds)}${row.label ? ` — ${row.label}` : ''}`;
      jump.setAttribute('aria-label', `Jump to ${jump.textContent}`);
      jump.addEventListener('click', () => {
        if (!valid(epoch) || pending) return;
        const element = getElement();
        if (!element || element.readyState < 1 || element.error) {
          message('Playback is not ready. Try jumping again after it loads.');
          return;
        }
        try {
          const upper = Number.isFinite(element.duration) && element.duration > 0 ? element.duration : Infinity;
          element.currentTime = Math.min(upper, row.position_seconds);
          close();
        } catch { message('Could not jump to this position. Try again.'); }
      });
      const remove = document.createElement('button');
      remove.type = 'button';
      remove.className = 'btn btn-danger btn-sm';
      remove.textContent = 'Delete';
      remove.setAttribute('aria-label', `Delete bookmark ${jump.textContent}`);
      remove.addEventListener('click', () => removeRow(row));
      item.append(jump, remove);
      list.append(item);
    }
    controls();
  }
  async function load() {
    const token = epoch;
    const id = mediaId;
    loading = true;
    loaded = false;
    retry.hidden = true;
    message('Loading bookmarks…');
    controls();
    try {
      const result = bookmarkRows(await api.bookmarks(id), id);
      if (!valid(token)) return;
      rows = result;
      loaded = true;
      render();
      message(rows.length ? '' : 'No saved positions yet.');
    } catch {
      if (!valid(token)) return;
      message('Could not load bookmarks. Try again.');
      retry.hidden = false;
    } finally {
      if (valid(token)) { loading = false; controls(); }
    }
  }
  async function removeRow(row) {
    const token = epoch;
    if (!valid(token) || pending) return;
    pending = true;
    controls();
    try {
      await api.deleteBookmark(mediaId, row.id);
      if (!valid(token)) return;
      rows = rows.filter(item => item.id !== row.id);
      render();
      message(rows.length ? 'Bookmark deleted.' : 'No saved positions yet.');
    } catch {
      if (valid(token)) message('Could not delete bookmark. Try again.');
    } finally {
      if (valid(token)) { pending = false; controls(); }
    }
  }
  function open(focusLabel) {
    const media = getMedia();
    if (!available() || !['audio', 'video'].includes(media?.type)) return;
    epoch++;
    mediaId = media.id;
    byId('bookmarks-media').textContent = media.file_name || 'Current media';
    rows = [];
    pending = false;
    label.value = '';
    list.replaceChildren();
    modal.classList.add('open');
    capturePosition();
    load();
    (focusLabel ? label : byId('bookmarks-close')).focus();
  }
  form.addEventListener('submit', async event => {
    event.preventDefault();
    const token = epoch;
    if (!valid(token) || save.disabled || pending) return;
    const submitted = label.value;
    const trimmed = submitted.trim();
    if ([...trimmed].length > 200) { message('Use at most 200 characters for the label.'); label.focus(); return; }
    pending = true;
    controls();
    try {
      const row = await api.saveBookmark(mediaId, position, trimmed);
      if (!valid(token)) return;
      rows = bookmarkRows([...rows, row], mediaId);
      render();
      if (label.value === submitted) label.value = '';
      message('Position saved.');
    } catch {
      if (valid(token)) message('Could not save position. Try again.');
    } finally {
      if (valid(token)) { pending = false; controls(); }
    }
  });
  capture.addEventListener('click', capturePosition);
  retry.addEventListener('click', load);
  byId('btn-save-position').addEventListener('click', () => open(true));
  byId('btn-bookmarks').addEventListener('click', () => open(false));
  byId('bookmarks-close').addEventListener('click', close);
  modal.addEventListener('click', event => { if (event.target === modal) close(); });
  modal.addEventListener('modalbeforeclose', () => { epoch++; mediaId = null; });
  // Handle Escape here too, so library and detached dialogs behave alike.
  modal.addEventListener('keydown', event => {
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(); }
  });
  return { setMedia(media) {
    close();
    const hidden = !['audio', 'video'].includes(media?.type);
    byId('btn-save-position').classList.toggle('hidden', hidden);
    byId('btn-bookmarks').classList.toggle('hidden', hidden);
  } };
}
