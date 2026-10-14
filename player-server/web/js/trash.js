import { API } from './api.js';
import { fmtDateTime } from './dom.js';
import { escapeHtml, toast } from './utils.js';
import { currentMediaId, stopAndClose } from './player.js';
import { state } from './state.js';

export function initTrash() {
  const modal = document.getElementById('trash-modal');
  document.getElementById('trash-toggle')?.addEventListener('click', async () => {
    modal.classList.add('open');
    await refreshTrash();
  });
  document.getElementById('trash-close')?.addEventListener('click', () => modal.classList.remove('open'));
  modal?.addEventListener('click', (e) => {
    if (e.target === modal) modal.classList.remove('open');
  });
  modal?.addEventListener('keydown', (e) => {
    if (e.ctrlKey || e.metaKey || e.altKey || e.target.matches?.('input, textarea, select') || e.target.isContentEditable) return;
    const delta = { j: 1, ArrowDown: 1, k: -1, ArrowUp: -1 }[e.key];
    if (!delta) return;
    const buttons = Array.from(modal.querySelectorAll('button[data-id]:not([disabled])'));
    if (!buttons.length) return;
    e.preventDefault();
    const index = buttons.indexOf(document.activeElement);
    buttons[Math.max(0, Math.min(buttons.length - 1, index < 0 ? 0 : index + delta))].focus();
  });
}

export async function moveToTrash(id, button) {
  if (!confirm('Move this item to trash? You can restore it for 7 days.')) return;
  if (button) button.disabled = true;
  try {
    await API.deleteMedia(id);
    if (String(currentMediaId()) === String(id)) stopAndClose();
    document.dispatchEvent(new CustomEvent('library:changed'));
    toast('Moved to trash; recoverable for 7 days');
  } catch (err) {
    if (button) {
      button.disabled = false;
      button.focus();
    }
    toast(err.message || 'Could not move item to trash', 'error');
  }
}

async function refreshTrash() {
  const list = document.getElementById('trash-list');
  list.textContent = 'Loading trash…';
  try {
    const items = await API.myTrash();
    if (!Array.isArray(items)) throw new Error('Invalid trash response');
    list.innerHTML = items.length ? `<ul class="admin-list">${items.map((item) => {
      const expires = new Date(new Date(item.deleted_at).getTime() + 7 * 86400000);
      const setName = state.sets.find((set) => set.id === item.set_id)?.name || `Set ${item.set_id}`;
      return `<li><span>${escapeHtml(item.file_name)}<br><span class="text-muted text-xs">${escapeHtml(setName)} / ${escapeHtml(item.rel_path)} · Deleted ${escapeHtml(fmtDateTime(item.deleted_at))} · Expires ${escapeHtml(fmtDateTime(expires.toISOString()))}</span></span><button class="btn btn-primary btn-sm" data-id="${item.id}" aria-label="Restore ${escapeHtml(item.file_name)}">Restore</button></li>`;
    }).join('')}</ul>` : '<p>No deleted items you can restore.</p>';
    list.querySelectorAll('button[data-id]').forEach((button) => {
      button.addEventListener('click', async () => {
        button.disabled = true;
        try {
          await API.restore(button.dataset.id);
          document.dispatchEvent(new CustomEvent('library:changed'));
          toast('Item restored');
          await refreshTrash();
        } catch (err) {
          button.disabled = false;
          button.focus();
          toast(err.message || 'Could not restore item', 'error');
        }
      });
    });
  } catch (err) {
    list.textContent = err.message || 'Could not load trash. Close and reopen to retry.';
  }
}
