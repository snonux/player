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
  const submitBtn = document.getElementById('upload-submit');
  const status = document.getElementById('upload-status');
  const statusText = document.getElementById('upload-status-text');
  const progress = document.getElementById('upload-progress');
  let uploading = false;

  const setUploading = (busy) => {
    uploading = busy;
    if (submitBtn) submitBtn.disabled = busy;
    if (fileInput) fileInput.disabled = busy;
    if (closeBtn) closeBtn.disabled = busy;
    form?.setAttribute('aria-busy', String(busy));
    status?.classList.toggle('hidden', !busy);
    progress?.classList.toggle('hidden', !busy);
    if (busy && statusText) statusText.textContent = 'Uploading…';
  };

  closeBtn?.addEventListener('click', () => {
    if (!uploading) modal?.classList.remove('open');
  });
  modal?.addEventListener('click', (e) => {
    if (e.target === modal && !uploading) modal.classList.remove('open');
  });
  modal?.addEventListener('modalbeforeclose', (e) => {
    if (uploading) e.preventDefault();
  });

  form?.addEventListener('submit', async (e) => {
    e.preventDefault();
    if (uploading) return;
    const setId = state.selectedSetId;
    if (!setId) {
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
    setUploading(true);
    try {
      await API.upload(setId, fd);
      toast('Upload complete');
      fileInput.value = '';
      status?.classList.add('hidden');
      progress?.classList.add('hidden');
      modal?.classList.remove('open');
      loadMediaCallback();
    } catch (err) {
      toast(err.message || 'Upload failed', 'error');
      if (statusText) statusText.textContent = 'Upload failed. Try again.';
      status?.classList.remove('hidden');
      progress?.classList.add('hidden');
    } finally {
      uploading = false;
      if (submitBtn) submitBtn.disabled = false;
      if (fileInput) fileInput.disabled = false;
      if (closeBtn) closeBtn.disabled = false;
      form?.setAttribute('aria-busy', 'false');
    }
  });
}

export function showUpload() {
  const modal = document.getElementById('upload-modal');
  if (document.getElementById('upload-form')?.getAttribute('aria-busy') !== 'true') {
    document.getElementById('upload-status')?.classList.add('hidden');
  }
  modal?.classList.add('open');
}
