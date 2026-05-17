import { API } from '../api.js';
import { state } from '../state.js';
import { fmtDate } from '../dom.js';
import { escapeHtml, toast } from '../utils.js';

export function initShares() {
  const modal = document.getElementById('shares-modal');
  const closeBtn = document.getElementById('shares-close');

  closeBtn?.addEventListener('click', () => closeSharesModal());
  modal?.addEventListener('click', (e) => {
    if (e.target === modal) closeSharesModal();
  });
}

function closeSharesModal() {
  const modal = document.getElementById('shares-modal');
  modal?.classList.remove('open');
  state.sharesCurrentRow = -1;
}

export async function toggleShares() {
  const modal = document.getElementById('shares-modal');
  if (!modal) return;
  if (modal.classList.contains('open')) {
    closeSharesModal();
    return;
  }
  try {
    const shares = await API.myShares();
    renderSharesList(shares || []);
    modal.classList.add('open');
  } catch (err) {
    toast(err.message || 'Failed to load shares', 'error');
  }
}

function renderSharesList(shares) {
  const el = document.getElementById('shares-list');
  if (!el) return;
  state.sharesData = shares;
  state.sharesCurrentRow = -1;
  if (!shares.length) {
    el.innerHTML = '<p class="text-muted text-sm">No share links yet.</p>';
    return;
  }
  const now = new Date();
  el.innerHTML = shares.map((sh, i) => {
    const expires = new Date(sh.expires_at);
    const expired = expires < now;
    const url = `${location.origin}/s/${sh.token}`;
    return `
      <div class="share-row flex gap-2 align-center py-1 border-b${i === 0 ? ' selected' : ''}" data-index="${i}" tabindex="0">
        <span class="flex-1 text-sm truncate">${escapeHtml(sh.file_name || 'Unknown')}</span>
        <span class="text-xs text-muted">${sh.media_type === 'video' ? '🎬' : sh.media_type === 'image' ? '🖼️' : '🎵'} ${expired ? '<span class="text-danger">Expired</span>' : fmtDate(expires)}</span>
        <button class="icon-btn btn-sm" title="Copy link" data-copy="${url}">&#128203;</button>
        <button class="icon-btn btn-sm" title="Revoke" data-revoke="${sh.token}">&#10005;</button>
      </div>
    `;
  }).join('');
  state.sharesCurrentRow = 0;
  updateSharesSelection();
  el.querySelectorAll('[data-copy]').forEach((b) => {
    b.addEventListener('click', () => {
      navigator.clipboard?.writeText(b.dataset.copy);
      toast('Link copied');
    });
  });
  el.querySelectorAll('[data-revoke]').forEach((b) => {
    b.addEventListener('click', async () => {
      try {
        await API.revokeShare(b.dataset.revoke);
        toast('Share revoked');
        const shares = await API.myShares();
        renderSharesList(shares || []);
      } catch (err) {
        toast(err.message || 'Revoke failed', 'error');
      }
    });
  });
}

function updateSharesSelection() {
  const rows = document.querySelectorAll('#shares-list .share-row');
  rows.forEach((r, i) => {
    r.classList.toggle('selected', i === state.sharesCurrentRow);
    if (i === state.sharesCurrentRow) r.focus();
  });
}

export function sharesNav(delta) {
  const rows = document.querySelectorAll('#shares-list .share-row');
  if (!rows.length) return;
  state.sharesCurrentRow = Math.max(0, Math.min(rows.length - 1, state.sharesCurrentRow + delta));
  updateSharesSelection();
}

export function copySelectedShare() {
  const rows = document.querySelectorAll('#shares-list .share-row');
  const row = rows[state.sharesCurrentRow];
  if (!row) return;
  const copyBtn = row.querySelector('[data-copy]');
  if (!copyBtn) {
    toast('Nothing to copy');
    return;
  }
  navigator.clipboard?.writeText(copyBtn.dataset.copy).then(() => toast('Share link copied'));
}

export async function deleteSelectedShare() {
  const rows = document.querySelectorAll('#shares-list .share-row');
  const row = rows[state.sharesCurrentRow];
  if (!row) {
    toast('No share selected');
    return;
  }
  const revokeBtn = row.querySelector('[data-revoke]');
  if (!revokeBtn) return;
  try {
    await API.revokeShare(revokeBtn.dataset.revoke);
    toast('Share revoked');
    const shares = await API.myShares();
    renderSharesList(shares || []);
  } catch (err) {
    toast(err.message || 'Revoke failed', 'error');
  }
}

export function isSharesOpen() {
  return document.getElementById('shares-modal')?.classList.contains('open');
}
