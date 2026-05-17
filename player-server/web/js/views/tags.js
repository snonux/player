import { API } from '../api.js';
import { currentElement } from '../selection.js';
import { state } from '../state.js';
import { escapeHtml, toast } from '../utils.js';

let tagsCurrentMediaId = null;
let tagFilterCallback = () => {};
let cachedTags = [];

export function initTags(options = {}) {
  tagFilterCallback = typeof options.onFilterChange === 'function' ? options.onFilterChange : (() => {});
  document.getElementById('tags-close')?.addEventListener('click', closeTagsModal);
  document.getElementById('tags-modal')?.addEventListener('click', (e) => {
    if (e.target === document.getElementById('tags-modal')) closeTagsModal();
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
  openTagsForElement(el);
}

export async function openTagsForElement(el) {
  const id = el.dataset.id;
  tagsCurrentMediaId = id;
  const detail = await API.mediaDetail(id);
  const tags = detail?.tags || [];
  renderTagsList(tags);
  document.getElementById('tags-modal')?.classList.add('open');
  document.getElementById('tags-new')?.focus();
}

export function closeTagsModal() {
  document.getElementById('tags-modal')?.classList.remove('open');
  tagsCurrentMediaId = null;
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
    b.addEventListener('click', async () => {
      if (!tagsCurrentMediaId) return;
      try {
        await API.removeTag(tagsCurrentMediaId, b.dataset.tag);
        const detail = await API.mediaDetail(tagsCurrentMediaId);
        renderTagsList(detail?.tags || []);
        refreshTagFilter();
      } catch (err) {
        toast(err.message || 'Remove tag failed', 'error');
      }
    });
  });
}

async function addTagForSelected() {
  if (!tagsCurrentMediaId) return;
  const input = document.getElementById('tags-new');
  const name = input?.value.trim();
  if (!name) return;
  try {
    await API.addTag(tagsCurrentMediaId, name);
    input.value = '';
    const detail = await API.mediaDetail(tagsCurrentMediaId);
    renderTagsList(detail?.tags || []);
    refreshTagFilter();
  } catch (err) {
    toast(err.message || 'Add tag failed', 'error');
  }
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
