import { API } from './api.js';

let currentMediaId = null;
let savedContent = '';
let revision = 0;
let openRevision = 0;

export function initNotes(onSave) {
  const modal = document.getElementById('notes-modal');
  const closeBtn = document.getElementById('notes-close');
  const saveBtn = document.getElementById('notes-save');
  const delBtn = document.getElementById('notes-delete');
  const area = document.getElementById('notes-textarea');
  let writePending = false;
  const setWritePending = (pending) => {
    writePending = pending;
    if (saveBtn) saveBtn.disabled = pending;
    if (delBtn) delBtn.disabled = pending;
  };

  const close = () => { modal.classList.remove('open'); currentMediaId = null; };
  const canDiscard = () => area.value === savedContent ||
    window.confirm('Discard your unsaved note changes?');
  area?.addEventListener('input', () => { revision += 1; });
  const dismiss = () => { if (canDiscard()) close(); };
  closeBtn?.addEventListener('click', dismiss);
  modal?.addEventListener('click', (e) => { if (e.target === modal) dismiss(); });
  modal?.addEventListener('modalbeforeclose', (e) => {
    if (!canDiscard()) e.preventDefault();
    else currentMediaId = null;
  });
  saveBtn?.addEventListener('click', async () => {
    if (!currentMediaId || writePending) return;
    setWritePending(true);
    const mediaId = currentMediaId;
    const content = area.value;
    const startedAt = revision;
    const openedAt = openRevision;
    try {
      await API.saveNote(mediaId, content);
      onSave?.('saved');
      if (currentMediaId === mediaId && openRevision === openedAt) {
        savedContent = content;
        if (revision === startedAt) close();
      }
    } catch (err) {
      onSave?.('error', err.message);
    } finally {
      setWritePending(false);
    }
  });
  delBtn?.addEventListener('click', async () => {
    if (!currentMediaId || writePending) return;
    setWritePending(true);
    const mediaId = currentMediaId;
    const startedAt = revision;
    const openedAt = openRevision;
    try {
      await API.deleteNote(mediaId);
      onSave?.('deleted');
      if (currentMediaId === mediaId && openRevision === openedAt) {
        savedContent = '';
        if (revision === startedAt) {
          area.value = '';
          close();
        }
      }
    } catch (err) {
      onSave?.('error', err.message);
    } finally {
      setWritePending(false);
    }
  });
}

export function open(mediaId, existingContent) {
  const modal = document.getElementById('notes-modal');
  const area = document.getElementById('notes-textarea');
  if (modal?.classList.contains('open')) {
    if (currentMediaId === mediaId) return;
    if (area?.value !== savedContent && !window.confirm('Discard your unsaved note changes?')) return;
  }
  revision += 1;
  openRevision += 1;
  currentMediaId = mediaId;
  savedContent = existingContent || '';
  if (area) area.value = savedContent;
  modal?.classList.add('open');
  document.getElementById('notes-textarea')?.focus();
}
