import { state } from '../state.js';
import { escapeHtml } from '../utils.js';
import { dismissSidebarAfterNav } from './help.js';

let loadMediaCallback = () => {};

export function initSets({ onLoadMedia } = {}) {
  loadMediaCallback = onLoadMedia || (() => {});
}

export function renderSets() {
  const el = document.getElementById('set-list');
  if (el) {
    // Sidebar stays navigation-only; regenerate-cover lives on set cards → More.
    el.innerHTML = state.sets.map((s) =>
      `<div class="set-row" tabindex="0" role="button" data-id="${s.id}" aria-label="Set ${escapeHtml(s.name)}">
        <span class="set-item flex-1-18" data-id="${s.id}">${escapeHtml(s.name)}</span>
      </div>`
    ).join('');
    el.querySelectorAll('.set-row').forEach((row) => {
      row.addEventListener('click', (e) => {
        e.preventDefault();
        if (selectSet(parseInt(row.dataset.id, 10))) dismissSidebarAfterNav();
      });
    });
  }
  updateSetRowsUI();
}

export function selectSet(id) {
  if (!Number.isFinite(id)) return false;
  if (!state.sets.some((set) => set.id === id)) return false;
  state.selectedSetIds = [id];
  state.selectedSetId = id;
  state.folderPath = '';
  updateSetRowsUI();
  loadMediaCallback();
  return true;
}

export function selectSetByHotkey(key) {
  const index = parseInt(key, 10) - 1;
  if (!Number.isFinite(index) || index < 0 || index >= 9 || index >= state.sets.length) {
    return false;
  }
  if (!selectSet(state.sets[index].id)) return false;
  dismissSidebarAfterNav();
  return true;
}

export function updateSetRowsUI() {
  document.querySelectorAll('#set-list .set-row').forEach((row) => {
    const id = parseInt(row.dataset.id, 10);
    const item = row.querySelector('.set-item');
    if (!item) return;
    item.classList.toggle('active', id === state.selectedSetId);
    item.classList.toggle('selected', state.selectedSetIds.includes(id));
  });
}

export function toggleSetSelection() {
  const sidebar = document.getElementById('sidebar');
  if (!sidebar) return;
  const focused = document.activeElement.closest('.set-row');
  if (!focused) return;
  const id = parseInt(focused.dataset.id, 10);
  const idx = state.selectedSetIds.indexOf(id);
  if (idx >= 0) {
    state.selectedSetIds.splice(idx, 1);
  } else {
    state.selectedSetIds.push(id);
  }
  if (state.selectedSetIds.length) {
    state.selectedSetId = state.selectedSetIds[state.selectedSetIds.length - 1];
  } else {
    state.selectedSetId = null;
  }
  state.folderPath = '';
  updateSetRowsUI();
  loadMediaCallback();
}

export function setSetByDelta(dx) {
  if (!state.sets.length) return;
  let idx = state.sets.findIndex((s) => s.id === state.selectedSetId);
  if (idx < 0) idx = 0;
  const nextIdx = (idx + dx + state.sets.length) % state.sets.length;
  selectSet(state.sets[nextIdx].id);
}
