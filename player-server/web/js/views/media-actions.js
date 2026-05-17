import { API } from '../api.js';
import { currentElement } from '../selection.js';
import { currentMediaId } from '../player.js';
import { open as openNotes } from '../notes.js';
import { toast } from '../utils.js';

export async function shareSelected() {
  const el = currentElement();
  if (!el) return;
  const id = el.dataset.id;
  try {
    const res = await API.share(id);
    const token = res?.token || res?.share?.token;
    const url = `${location.origin}/s/${token}`;
    navigator.clipboard?.writeText(url);
    toast('Share link copied');
  } catch (err) {
    toast(err.message || 'Share failed', 'error');
  }
}

export async function toggleFavorite(id, btn) {
  try {
    await API.favorite(id);
    btn?.classList.toggle('active');
  } catch (err) {
    toast(err.message || 'Favorite failed', 'error');
  }
}

async function setProgressStatus(id, status, successMessage) {
  const mediaId = id || selectedMediaId();
  if (!mediaId) return false;
  try {
    await API.progressStatus(mediaId, status);
    toast(successMessage);
    return true;
  } catch (err) {
    toast(err.message || 'Progress update failed', 'error');
    return false;
  }
}

export function markAsFinished(id) {
  return setProgressStatus(id, 'finished', 'Marked as finished');
}

export function markAsNotStarted(id) {
  return setProgressStatus(id, 'not_started', 'Marked as not started');
}

export async function openNotesForSelected() {
  const el = currentElement();
  if (!el) return;
  const id = el.dataset.id;
  let content = '';
  try {
    const note = await API.notes(id);
    content = note?.content || '';
  } catch {}
  openNotes(id, content);
}

export async function downloadSelected() {
  const el = currentElement();
  if (!el) return;
  window.open(`/api/media/${el.dataset.id}/download`, '_blank');
}

export function selectedMediaId() {
  const el = currentElement();
  if (el?.dataset?.id) return el.dataset.id;
  const id = currentMediaId();
  return id ? String(id) : '';
}

export async function regenThumb(mediaId) {
  try {
    await API.regenThumbnail(mediaId);
    toast('Thumbnail regenerated');
    document.querySelectorAll(`#media-grid [data-id="${mediaId}"] img`).forEach((img) => {
      const base = img.src.split('?')[0];
      img.src = `${base}?t=${Date.now()}`;
    });
    const e = document.getElementById('cover-art');
    if (e && !e.classList.contains('hidden')) {
      const base = e.src.split('?')[0];
      e.src = `${base}?t=${Date.now()}`;
    }
  } catch (err) {
    toast(err.message || 'Thumbnail failed', 'error');
  }
}
