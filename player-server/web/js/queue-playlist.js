import { API } from './api.js';
import { state } from './state.js';
import { openPlaylist } from './views/music-catalog.js';

// Capture occurrences once: subsequent playback/queue edits never change the copy.
export function saveQueueAsPlaylist(entries, returnFocus, openSaved) {
  const user = state.user, ids = entries.map(entry => entry.media.id);
  if (!user || !ids.length || ids.length > 10000) return;
  const modal = document.getElementById('playlist-modal');
  const parent = modal.parentElement;
  if (document.fullscreenElement) document.fullscreenElement.appendChild(modal);
  modal.innerHTML = `<div class="modal" role="dialog" aria-modal="true" aria-labelledby="queue-save-title"><div class="modal-header"><h2 id="queue-save-title">Save queue as playlist</h2></div><div class="modal-body"><form><label>Name<input name="name" aria-label="Playlist name" required autofocus></label><p>${ids.length} songs · Private playlist. Keeps this order and repeated songs.</p><p>Your queue keeps playing. If a request loses its connection, check Playlists before retrying to avoid a second copy.</p><p role="alert" data-error></p><p role="status" data-status></p><div class="playlist-actions"><button class="btn" type="button" data-close>Cancel</button><button class="btn btn-primary" type="submit">Save playlist</button></div></form></div></div>`;
  const own = modal.firstElementChild;
  const alive = () => modal.classList.contains('open') && modal.firstElementChild === own;
  const current = () => state.user === user;
  const close = () => { modal.removeEventListener('modalbeforeclose', close); modal.classList.remove('open', 'queue-playlist-modal'); if (modal.parentElement !== parent) parent.appendChild(modal); returnFocus(); };
  modal.addEventListener('modalbeforeclose', close);
  modal.querySelector('[data-close]').onclick = close;
  // A narrow queue is itself a modal; Escape dismisses only this naming dialog.
  const key = event => { if (event.key === 'Escape') { event.preventDefault(); event.stopImmediatePropagation(); close(); } };
  own.addEventListener('keydown', key, true);
  let busy = false;
  modal.querySelector('form').onsubmit = async event => {
    event.preventDefault();
    if (busy || !alive()) return;
    if (!current()) { close(); return; }
    const name = modal.querySelector('[name=name]').value.trim();
    const error = modal.querySelector('[data-error]'), status = modal.querySelector('[data-status]');
    if ([...name].length < 1 || [...name].length > 200) { error.textContent = 'Name must be 1–200 characters.'; return; }
    const button = modal.querySelector('[type=submit]');
    busy = true; button.disabled = true; error.textContent = ''; status.textContent = 'Saving playlist…';
    try {
      const saved = await API.createPlaylist({name, comment: '', visibility: 'private', media_ids: ids});
      if (!alive() || !current()) return;
      modal.querySelector('form').innerHTML = '<p role="status">Private playlist saved. Your queue keeps playing.</p><div class="playlist-actions"><button class="btn" type="button" data-close>Close</button><a class="btn btn-primary" data-open>Open playlist</a></div>';
      modal.querySelector('[data-close]').onclick = close;
      const link = modal.querySelector('[data-open]');
      link.href = `/?music=playlist&id=${encodeURIComponent(saved.playlist_id)}`;
      link.onclick = event => { if (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return; event.preventDefault(); if (!current()) { close(); return; } close(); openSaved(); openPlaylist(saved.playlist_id); };
      link.focus();
    } catch (e) {
      if (alive() && current()) { error.textContent = e.message || 'Could not save playlist. Check your connection.'; status.textContent = ''; }
    } finally { busy = false; if (alive() && button.isConnected) button.disabled = false; }
  };
  modal.classList.add('open', 'queue-playlist-modal');
  modal.querySelector('[name=name]').focus();
}
