import { API, NO_NOTE } from '../api.js';
import { currentElement } from '../selection.js';
import { currentMediaId } from '../player.js';
import { open as openNotes } from '../notes.js';
import { toast } from '../utils.js';
import { copyShareLink } from '../share-link.js';

let latestNoteLoad = 0;

// selectedCardMediaId returns the media ID of the selected grid card, or ''
// when the selection is a set, folder or podcast-episode card. Those carry no
// data-id, and item actions must not fall through to an unrelated media.
function selectedCardMediaId() {
  const id = currentElement()?.dataset?.id || '';
  if (!id) toast('Select a media item first', 'info');
  return id;
}

export async function shareSelected() {
  const id = selectedCardMediaId();
  if (!id) return;
  try {
    const res = await API.share(id);
    const token = res?.token || res?.share?.token;
    if (!token) throw new Error('Share link was not returned');
    const url = `${location.origin}/s/${token}`;
    await copyShareLink(url);
  } catch (err) {
    toast(err.message || 'Share failed', 'error');
  }
}

// toggleFavorite flips the favorite flag and shows the state the server
// reports, so the heart and the toast stay right even after a stale render.
export async function toggleFavorite(id, btn) {
  try {
    const res = await API.favorite(id);
    const favorite = typeof res?.favorite === 'boolean' ? res.favorite : !btn?.classList.contains('active');
    btn?.classList.toggle('active', favorite);
    btn?.setAttribute('aria-pressed', String(favorite));
    toast(favorite ? 'Added to favorites' : 'Removed from favorites');
  } catch (err) {
    toast(err.message || 'Favorite failed', 'error');
  }
}

async function setProgressStatus(id, status, successMessage) {
  const mediaId = id || selectedMediaId();
  if (!mediaId) {
    toast('Select a media item first', 'info');
    return false;
  }
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
  const id = selectedCardMediaId();
  if (!id) return;
  const request = ++latestNoteLoad;
  try {
    const note = await API.notes(id);
    if (request !== latestNoteLoad || currentElement()?.dataset.id !== id) return;
    if (note !== NO_NOTE && typeof note?.content !== 'string') {
      throw new Error('Invalid note response');
    }
    openNotes(id, note === NO_NOTE ? '' : note.content);
  } catch {
    if (request !== latestNoteLoad || currentElement()?.dataset.id !== id) return;
    toast('Could not load note. Try again.', 'error');
  }
}

export async function downloadSelected() {
  const id = selectedCardMediaId();
  if (!id) return;
  window.open(`/api/media/${id}/download`, '_blank');
}

// selectedMediaId prefers the selected media card and otherwise falls back to
// the playing media (e.g. i while a set or folder card is selected). A
// selected podcast-episode card yields '': its episode is not a media item,
// and mark-finished must not silently hit whatever is playing instead.
export function selectedMediaId() {
  const el = currentElement();
  if (el?.dataset?.id) return el.dataset.id;
  if (el?.classList.contains('episode-card')) return '';
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
