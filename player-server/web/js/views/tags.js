import { API } from '../api.js';
import { currentElement, focusSelectedCard } from '../selection.js';
import { state } from '../state.js';
import { escapeHtml, toast } from '../utils.js';

let tagsCurrentMediaId = null;
let tagLoadRevision = 0;
let tagFilterCallback = () => {};
let cachedTags = [];

export function initTags(options = {}) {
  tagFilterCallback = typeof options.onFilterChange === 'function' ? options.onFilterChange : (() => {});
  document.getElementById('tags-close')?.addEventListener('click', closeTagsModal);
  document.getElementById('tags-modal')?.addEventListener('click', (e) => {
    if (e.target === document.getElementById('tags-modal')) closeTagsModal();
  });
  document.getElementById('tags-modal')?.addEventListener('modalbeforeclose', () => {
    resetTagsModal();
  });
  document.getElementById('tags-add')?.addEventListener('click', addTagForSelected);
  document.getElementById('tags-new')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      addTagForSelected();
    }
  });
  document.addEventListener('filters:changed', renderTagFilter);
  refreshTagFilter();
}

export async function openTagsForSelected() {
  const el = currentElement();
  if (!el) return;
  await openTagsForElement(el);
}

export async function openTagsForElement(el) {
  const id = el.dataset.id;
  if (!id) return;
  const revision = ++tagLoadRevision;
  try {
    const detail = await API.mediaDetail(id);
    if (revision !== tagLoadRevision) return;
    if (!detail?.media || String(detail.media.id) !== String(id)) {
      throw new Error('Media details unavailable');
    }
    tagsCurrentMediaId = id;
    renderTagsList(detail.tags || []);
    document.getElementById('tags-modal')?.classList.add('open');
    document.getElementById('tags-new')?.focus();
  } catch (err) {
    if (revision === tagLoadRevision) toast(err.message || 'Failed to load tags', 'error');
  }
}

// Abandons a tag dialog that is still loading, e.g. after Escape, so a slow
// response cannot open the dialog and steal focus once the user moved on.
export function cancelTagsLoad() {
  tagLoadRevision++;
}

export function closeTagsModal() {
  document.getElementById('tags-modal')?.classList.remove('open');
  resetTagsModal();
}

function resetTagsModal() {
  tagLoadRevision++;
  tagsCurrentMediaId = null;
  // Cards are keyboard targets; returning focus to the hidden input traps
  // global shortcuts such as / after the dialog closes.
  queueMicrotask(() => {
    if (!document.getElementById('tags-modal')?.classList.contains('open')) {
      if (!focusSelectedCard()) document.getElementById('media-grid')?.focus();
    }
  });
}

function renderTagsList(tags) {
  const el = document.getElementById('tags-list');
  if (!el) return;
  if (!tags.length) {
    el.innerHTML = '<span class="text-muted text-xs">No tags.</span>';
    return;
  }
  el.innerHTML = tags.map((t) =>
    `<span class="tag-chip">${escapeHtml(t.name)} <button class="icon-btn btn-sm tag-remove" data-tag="${escapeHtml(t.name)}" title="Remove">✕</button></span>`
  ).join('');
  el.querySelectorAll('.tag-remove').forEach((b) => {
    b.addEventListener('click', () => {
      mutateTags((id) => API.removeTag(id, b.dataset.tag), 'Remove tag failed');
    });
  });
}

async function addTagForSelected() {
  const input = document.getElementById('tags-new');
  const name = input?.value.trim();
  if (!name) return;
  if (await mutateTags((id) => API.addTag(id, name), 'Add tag failed')) input.value = '';
}

// Applies a tag change to the media shown in the dialog. The media ID and the
// dialog revision are captured before any await: closing or reopening the
// dialog mid-request must not turn a saved change into an error or redraw
// another item's tags. Tag-filtered results refresh even after the dialog
// closed, so the grid never shows media that no longer match.
async function mutateTags(mutate, failureMessage) {
  const id = tagsCurrentMediaId;
  if (!id) return false;
  const revision = tagLoadRevision;
  try {
    await mutate(id);
  } catch (err) {
    toast(err.message || failureMessage, 'error');
    return false;
  }
  refreshTagFilter();
  if (state.filters.tags) tagFilterCallback();
  if (revision !== tagLoadRevision) return true;
  try {
    const detail = await API.mediaDetail(id);
    if (revision === tagLoadRevision) renderTagsList(detail?.tags || []);
  } catch (err) {
    toast(err.message || 'Tags saved, but reloading them failed', 'error');
  }
  return true;
}

async function refreshTagFilter() {
  const el = document.getElementById('tag-filter-list');
  if (!el) return;
  try {
    cachedTags = await API.tags();
    renderTagFilter();
  } catch {
    cachedTags = [];
    renderTagFilter();
  }
}

function renderTagFilter() {
  const el = document.getElementById('tag-filter-list');
  if (!el) return;
  const active = selectedTags();
  el.innerHTML = (cachedTags || []).map((tag) => {
    const name = tag.name || '';
    const isActive = active.includes(name);
    return `<button type="button" class="tag-filter-chip${isActive ? ' active' : ''}" data-tag="${escapeHtml(name)}" title="Filter tag ${escapeHtml(name)}"><span>#${escapeHtml(name)}</span></button>`;
  }).join('');
  el.querySelectorAll('.tag-filter-chip').forEach((button) => {
    button.addEventListener('click', () => toggleTagFilter(button.dataset.tag));
  });
}

function toggleTagFilter(name) {
  if (!name) return;
  const tags = selectedTags();
  const next = tags.includes(name)
    ? tags.filter((tag) => tag !== name)
    : [...tags, name];
  state.filters.tags = next.join(',');
  state.folderPath = '';
  renderTagFilter();
  tagFilterCallback();
}

function selectedTags() {
  return (state.filters.tags || '').split(',').map((tag) => tag.trim()).filter(Boolean);
}
