import { API } from '../api.js';
import { state } from '../state.js';
import { toast } from '../utils.js';

let loadMediaCallback = () => {};

export function initUpload({ onLoadMedia } = {}) {
  loadMediaCallback = onLoadMedia || (() => {});
  const modal = document.getElementById('upload-modal');
  const closeBtn = document.getElementById('upload-close');
  const form = document.getElementById('upload-form');
  const fileInput = document.getElementById('upload-file');

  closeBtn?.addEventListener('click', () => modal?.classList.remove('open'));
  modal?.addEventListener('click', (e) => {
    if (e.target === modal) modal.classList.remove('open');
  });

  form?.addEventListener('submit', async (e) => {
    e.preventDefault();
    if (!state.selectedSetId) {
      toast('Select a set first', 'error');
      return;
    }
    const file = fileInput?.files[0];
    if (!file) {
      toast('Choose a file', 'error');
      return;
    }
    const fd = new FormData();
    fd.append('file', file);
    try {
      await API.upload(state.selectedSetId, fd);
      toast('Upload complete');
      fileInput.value = '';
      modal?.classList.remove('open');
      loadMediaCallback();
    } catch (err) {
      toast(err.message || 'Upload failed', 'error');
    }
  });
}

export function showUpload() {
  const modal = document.getElementById('upload-modal');
  modal?.classList.add('open');
}
