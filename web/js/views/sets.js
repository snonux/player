import { API } from '../api.js';
import { state } from '../state.js';
import { escapeHtml, toast } from '../utils.js';

let loadMediaCallback = () => {};

export function initSets({ onLoadMedia } = {}) {
  loadMediaCallback = onLoadMedia || (() => {});
}

export function renderSets() {
  const el = document.getElementById('set-list');
  if (!el) return;
  el.innerHTML = state.sets.map((s) =>
    `<div class="set-row" tabindex="0" role="button" data-id="${s.id}" aria-label="Set ${escapeHtml(s.name)}">
      <span class="set-item flex-1-18" data-id="${s.id}">${escapeHtml(s.name)}</span>
      <button class="icon-btn btn-sm" data-cover-set="${s.id}" title="Regenerate cover">🔄</button>
    </div>`
  ).join('');
  el.querySelectorAll('.set-row').forEach((row) => {
    row.addEventListener('click', (e) => {
      if (e.target.closest('[data-cover-set]')) return;
      e.preventDefault();
      const id = parseInt(row.dataset.id, 10);
      state.selectedSetIds = [id];
      state.selectedSetId = id;
      state.folderPath = '';
      updateSetRowsUI();
      loadMediaCallback();
    });
  });
  el.querySelectorAll('[data-cover-set]').forEach((b) => {
    b.addEventListener('click', async (e) => {
      e.stopPropagation();
      const id = b.dataset.coverSet;
      try {
        await API.regenCover(id);
        toast('Cover regenerated');
      } catch (err) {
        toast(err.message || 'Cover failed', 'error');
      }
    });
  });
  updateSetRowsUI();
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

function setActiveSet(id) {
  document.querySelectorAll('.set-item').forEach((a) => a.classList.toggle('active', parseInt(a.dataset.id, 10) === id));
}

export function setSetByDelta(dx) {
  if (!state.sets.length) return;
  let idx = state.sets.findIndex((s) => s.id === state.selectedSetId);
  if (idx < 0) idx = 0;
  const nextIdx = (idx + dx + state.sets.length) % state.sets.length;
  state.selectedSetId = state.sets[nextIdx].id;
  state.selectedSetIds = [state.selectedSetId];
  state.folderPath = '';
  setActiveSet(state.selectedSetId);
  loadMediaCallback();
}
