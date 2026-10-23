import { downloadMusic } from './music-downloads.js';
import { queueCollectionSelected } from './collection-actions.js';
import { insertQueue, undoQueue, queueState, queueUndoToken } from './queue.js';
import { state } from './state.js';
import { currentElement } from './selection.js';
import { toast, escapeHtml as esc } from './utils.js';

export function songActionButtons(song) {
  return `<span hidden>${[['now','Play now'],['next','Play next'],['append','Add to queue']].map(([action,label]) => `<button data-action="queue-${action}" aria-label="${label}" ${song.type === 'audio' ? '' : 'disabled'}>${label}</button>`).join('')}</span>`;
}
export function bindSongActions(root) {
  root.querySelectorAll('[data-action^="queue-"]').forEach(button => {
    button.onclick = e => {
      e.stopPropagation();
      const id = button.closest('[data-id]')?.dataset.id;
      void queueSongs(button.dataset.action.slice(6), [state.media.find(song => String(song.id) === id)].filter(Boolean));
    };
  });
}
export async function queueSongs(action, songs) {
  if (!songs.length) { toast('Select a song first', 'info'); return; }
  if (songs.some(song => song.type !== 'audio')) { toast('Only audio can be queued', 'info'); return; }
  if (queueState().loading) { toast('Loading track… Please wait.', 'info'); return; }
  if (await insertQueue(songs, action)) { const token = queueUndoToken(); if (!token) return; toast(`Added ${songs.length} ${songs.length === 1 ? 'song' : 'songs'}`, 'info', {label:'Undo',run:() => undoQueue(token)}); }
}
export function queueSelected(action) {
  if (queueCollectionSelected(action)) return;
  const checked = [...document.querySelectorAll('#media-grid [data-song-check]:checked')].map(input => input.dataset.songCheck);
  const id = currentElement()?.dataset.id;
  const songs = checked.length ? state.media.filter(song => checked.includes(String(song.id))) : state.media.filter(song => String(song.id) === id);
  void queueSongs(action, songs);
}
export function openSongActions(row) { document.dispatchEvent(new CustomEvent('media:actions', {detail:row})); }
export function selectionActions(button, songs) {
  const menu = document.createElement('div'); menu.className = 'shortcut-picker';
  menu.setAttribute('popover','auto'); menu.setAttribute('role','group'); menu.setAttribute('aria-label','Selected song actions');
  menu.innerHTML = [['now','Play now'],['next','Play next'],['append','Add to queue'],['download','Download…']].map(([action,label]) => `<button class="btn btn-ghost" data-insert="${action}">${esc(label)}</button>`).join('');
  menu.querySelectorAll('button').forEach(item => item.onclick = () => { menu.hidePopover(); if(item.dataset.insert==='download')downloadMusic(songs,button);else void queueSongs(item.dataset.insert,songs); });
  menu.addEventListener('toggle', e => { if (e.newState === 'closed') { menu.remove(); if (!document.querySelector('.modal-overlay.open')) button.focus(); } });
  document.body.append(menu); menu.showPopover(); menu.querySelector('button').focus();
}
