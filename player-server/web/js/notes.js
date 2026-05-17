import { API } from './api.js';

let currentMediaId = null;

export function initNotes(onSave) {
  const modal = document.getElementById('notes-modal');
  const closeBtn = document.getElementById('notes-close');
  const saveBtn = document.getElementById('notes-save');
  const delBtn = document.getElementById('notes-delete');
  const area = document.getElementById('notes-textarea');

  const close = () => { modal.classList.remove('open'); currentMediaId = null; };
  closeBtn?.addEventListener('click', close);
  modal?.addEventListener('click', (e) => { if (e.target === modal) close(); });
  saveBtn?.addEventListener('click', async () => {
    if (!currentMediaId) return;
    try {
      await API.saveNote(currentMediaId, area.value);
      onSave?.('saved');
      close();
    } catch (err) {
      onSave?.('error', err.message);
    }
  });
  delBtn?.addEventListener('click', async () => {
    if (!currentMediaId) return;
    try {
      await API.deleteNote(currentMediaId);
      area.value = '';
      onSave?.('deleted');
      close();
    } catch (err) {
      onSave?.('error', err.message);
    }
  });
}

export function open(mediaId, existingContent) {
  currentMediaId = mediaId;
  const modal = document.getElementById('notes-modal');
  const area = document.getElementById('notes-textarea');
  if (area) area.value = existingContent || '';
  modal?.classList.add('open');
  document.getElementById('notes-textarea')?.focus();
}
