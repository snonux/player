import { API } from './api.js';
import { state } from './state.js';
import { escapeHtml as esc } from './utils.js';
import { html, t } from './i18n.js';

// Empty description may be omitted on older payloads; never keep stale list text.
export function mergeShareEdit(existing, updated) {
  return { ...existing, ...updated, description: updated.description ?? '' };
}

// A local datetime input is only submitted when changed, preserving the exact
// stored instant (including seconds) on description-only edits.
export function localShareExpiry(value) {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return '';
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}

export function editShare(container, share, onClose) {
  const user = state.user;
  const initialExpiry = localShareExpiry(share.expires_at);
  container.innerHTML = html`<form data-share-editor class="py-1"><h3>Edit share</h3><p class="text-sm">${esc(share.file_name || '')}</p><label>Description<textarea name="description" rows="3" aria-label="Description"></textarea></label><label>Link expires (local time)<input type="datetime-local" name="expiry" required aria-label="Link expires (local time)"></label><p class="text-sm text-muted">The link and viewing limits stay the same.</p><p role="status" data-status></p><div class="playlist-actions"><button type="button" class="btn" data-cancel>Cancel</button><button type="submit" class="btn btn-primary">Save</button></div></form>`;
  const form = container.querySelector('form'), status = form.querySelector('[data-status]');
  const setStatus = key => {
    status.dataset.i18nText = JSON.stringify({ key });
    status.textContent = t(key);
  };
  form.elements.description.value = share.description || '';
  form.elements.expiry.value = initialExpiry;
  let saving = false;
  const close = () => { if (!saving) onClose(); };
  form.querySelector('[data-cancel]').onclick = close;
  form.addEventListener('keydown', event => {
    // The shares modal owns j/k/Enter globally; typing and native buttons here
    // must never trigger list actions or close the parent dialog.
    event.stopPropagation();
    if (event.key === 'Escape') { event.preventDefault(); close(); }
  });
  form.onsubmit = async event => {
    event.preventDefault();
    if (saving) return;
    const patch = { description: form.elements.description.value };
    if ([...patch.description].length > 1000) { setStatus('Description must be at most 1000 characters.'); return; }
    if (form.elements.expiry.value !== initialExpiry) {
      const date = new Date(form.elements.expiry.value);
      if (!Number.isFinite(date.getTime()) || date <= new Date()) { setStatus('Choose a future expiry.'); return; }
      patch.expires_at = date.toISOString();
    }
    saving = true;
    form.querySelectorAll('button,input,textarea').forEach(control => { control.disabled = true; });
    setStatus('Saving…');
    try {
      if (state.user !== user) throw Error(t('Account changed. Close this dialog and try again.'));
      const updated = await API.updateShare(share.token, patch);
      if (!form.isConnected || state.user !== user) return;
      onClose(updated);
    } catch (_) {
      if (!form.isConnected) return;
      setStatus('Could not save share. Check your connection and edit permission, then retry.');
      saving = false;
      form.querySelectorAll('button,input,textarea').forEach(control => { control.disabled = false; });
    }
  };
  form.elements.description.focus();
}
