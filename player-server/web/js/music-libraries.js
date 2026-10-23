import { API } from './api.js';
import { escapeHtml, toast } from './utils.js';
import { ui } from './i18n.js';

const STORAGE_KEY = 'player.music.library_ids';

let cached = null;

export function selectedLibraryIds() {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const ids = JSON.parse(raw);
    return Array.isArray(ids) ? ids.filter((id) => Number.isFinite(id) && id > 0) : [];
  } catch {
    return [];
  }
}

export function libraryQueryParams() {
  const ids = selectedLibraryIds();
  return ids.length ? { library_ids: ids.join(',') } : {};
}

export async function loadMusicLibraries() {
  if (cached) return cached;
  cached = await API.musicLibraries();
  return cached;
}

export function clearMusicLibraryCache() {
  cached = null;
}

/** Renders a library multi-select only when the caller can see more than one. */
export async function mountMusicLibraryFilter(host, onChange) {
  if (!host) return;
  let existing = host.querySelector('[data-music-libraries]');
  if (existing) existing.remove();
  let libs;
  try {
    libs = await loadMusicLibraries();
  } catch (err) {
    toast(err.message || ui('Could not load libraries.'), 'error');
    return;
  }
  if (!Array.isArray(libs) || libs.length <= 1) return;
  const selected = new Set(selectedLibraryIds());
  const wrap = document.createElement('div');
  wrap.className = 'music-libraries grid-full';
  wrap.dataset.musicLibraries = '1';
  wrap.innerHTML = `<label class="text-xs">${ui('Libraries')}
    <select multiple size="${Math.min(libs.length, 4)}" aria-label="${ui('Filter music by library')}">
      ${libs.map((lib) => `<option value="${lib.id}" ${selected.has(lib.id) ? 'selected' : ''}>${escapeHtml(lib.name)}</option>`).join('')}
    </select>
  </label>
  <p class="text-muted text-xs">${ui('Leave empty for all accessible libraries.')}</p>`;
  const select = wrap.querySelector('select');
  select.addEventListener('change', () => {
    const ids = [...select.selectedOptions].map((o) => Number(o.value)).filter((id) => id > 0);
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify(ids));
    onChange?.(ids);
  });
  host.prepend(wrap);
}
