import { cancelPendingSearch } from '../search.js';
import { updateSetRowsUI } from './sets.js';
import { API } from '../api.js';
import { state } from '../state.js';
import { escapeHtml as esc, fmtDur, toast } from '../utils.js';
import { mediaTitle } from '../music.js';
import { selectByElement, focusSelectedCard } from '../selection.js';
import { playAlbumCollection } from './playback-nav.js';

let reload = () => {};
let selectedAlbum = '';
let revision = 0;
export function initMusicCatalog(load) {
 reload = load;
 document.getElementById('music-albums')?.addEventListener('click', () => openAlbums());
 document.addEventListener('music:album', e => openAlbum(e.detail));
}
export function openAlbums(artist = '', name = '') {
 cancelPendingSearch();
 state.music = { artist, name, album: '' };
 state.virtualSet = ''; state.selectedSetId = null; state.selectedSetIds = []; state.folderPath = '';
 document.getElementById('sidebar')?.classList.remove('open');
 document.querySelector('.page')?.classList.remove('has-sidebar');
 updateSetRowsUI();
 reload();
}
export function openAlbum(key) {
 cancelPendingSearch();
 if (!state.music) state.music = {artist:'',name:'',album:''};
 selectedAlbum = key;
 state.music.album = key;
 reload();
}
export function backMusic() {
 if (!state.music) return false;
 if (state.music.album) state.music.album = '';
 else if (state.music.artist) { state.music.artist = ''; state.music.name = ''; }
 else { state.music = null; }
 reload();
 return true;
}
export async function openSongAlbum(id) {
 try {
  const albums = await API.albums({media_id:id});
  if (!albums.length) { toast('No album for this item.', 'info'); return; }
  openAlbums(); openAlbum(albums[0].album_key);
 } catch (e) { toast(e.message || 'Could not open album.', 'error'); }
}
export async function renderMusic(grid, isCurrent) {
 const operation = ++revision;
 const view = {...state.music};
 const current = () => operation===revision && isCurrent() && state.music;
 state.media = [];
 document.getElementById('breadcrumb-bar')?.classList.add('hidden');
 const count = document.getElementById('result-count');
 count.textContent = 'Music · Albums';
 grid.classList.toggle('music-detail', Boolean(view.album));
 grid.innerHTML = '<p class="grid-full" role="status">Loading albums…</p>';
 try {
  const data = view.album ? await API.album(view.album) : await API.albums(view.artist ? {artist:view.artist} : {});
  if (!current()) return;
  if (view.album) {
   state.media = data.songs || [];
   count.textContent = `Music · Albums · ${data.title}`;
   grid.innerHTML = `<div class="grid-full music-heading"><button class="btn btn-ghost" data-music-back>← Back to albums</button><div class="album-header">${cover(data)}<div><h1>${esc(data.title)}</h1><button class="btn btn-ghost" data-artist="${esc(data.artist_key)}">${esc(data.artist)}</button><p class="text-muted">${data.year || ''}${data.from_folder ? ' · From folder' : ''}</p><p>${data.song_count} songs · ${fmtDur(data.duration)}</p><button class="btn btn-primary" data-album-play>▶ Play album</button></div></div></div>` + state.media.map((song,index) => `<div class="media-row" role="group" tabindex="0" data-id="${song.id}" data-index="${index}" aria-label="Play ${esc(mediaTitle(song))}"><span class="text-muted music-number">${song.music?.disc ? `${song.music.disc} · ` : ''}${song.music?.track || '—'}</span><span class="music-song"><button class="btn btn-ghost song-open" title="${esc(mediaTitle(song))}">${esc(mediaTitle(song))}</button><span class="text-muted text-sm">${esc(song.music?.artist || 'Unknown artist')}</span></span><span>${fmtDur(song.duration)}</span><button class="btn btn-ghost" data-song-info="${song.id}" aria-label="Info for ${esc(mediaTitle(song))}">Info</button></div>`).join('');
   grid.querySelector('[data-album-play]').onclick = () => playAlbumCollection(data.songs,0,data.title);
   grid.querySelector('[data-artist]').onclick = () => openAlbums(data.artist_key,data.artist);
   grid.querySelectorAll('.media-row').forEach(row => {
    row.addEventListener('click', e => { if (e.target.closest('[data-song-info]')) return; selectByElement(row); playAlbumCollection(data.songs,Number(row.dataset.index),data.title); });
   });
   grid.querySelectorAll('[data-song-info]').forEach(button => button.onclick = () => { selectByElement(button.closest('.media-row')); document.dispatchEvent(new Event('music:song-info')); });
  } else {
   count.textContent = view.artist ? `Albums by ${view.name || 'artist'}` : `Music · ${data.length} albums`;
   grid.innerHTML = `<div class="grid-full"><button class="btn btn-ghost" data-music-back>← ${view.artist ? 'Back to albums' : 'Back to Sets'}</button>${view.artist ? `<h1>Albums by ${esc(view.name || 'artist')}</h1>` : ''}</div>` + (data.length ? data.map(album => `<div class="media-card album-card" role="button" tabindex="0" data-album="${esc(album.album_key)}" aria-label="Open album ${esc(album.title)} by ${esc(album.artist)}">${cover(album)}<div class="card-body"><div class="title" title="${esc(album.title)}">${esc(album.title)}</div><div class="text-muted text-sm">${esc(album.artist)}${album.year ? ` · ${album.year}` : ''}</div>${album.from_folder ? '<span class="text-muted text-xs">From folder</span>' : ''}</div></div>`).join('') : '<p class="grid-full">No albums available. Music includes audio from your permitted sets. Browse Sets or ask an administrator to rescan music tags.</p>');
   grid.querySelectorAll('[data-album]').forEach(card => card.onclick = () => openAlbum(card.dataset.album));
  }
  grid.querySelector('[data-music-back]').onclick = backMusic;
  const target = !view.album && selectedAlbum ? grid.querySelector(`[data-album="${CSS.escape(selectedAlbum)}"]`) : grid.querySelector('.media-row, .album-card');
  if (target) { selectByElement(target); focusSelectedCard(); } else grid.querySelector('[data-music-back]')?.focus();
 } catch (e) {
  if (!current()) return;
  grid.innerHTML = `<div class="grid-full" role="alert"><p>Could not load albums: ${esc(e.message || 'Connection unavailable. Check your connection.')}</p><button class="btn" data-retry>Retry</button><button class="btn btn-ghost" data-music-back>← Back</button></div>`;
  grid.querySelector('[data-retry]').onclick = reload;
  grid.querySelector('[data-music-back]').onclick = backMusic;
 }
}
function cover(album) {
 return album.cover_media_id ? `<img class="album-cover" src="/api/media/${album.cover_media_id}/thumbnail" alt="Artwork for ${esc(album.title)}">` : '<div class="album-cover album-placeholder" role="img" aria-label="No album artwork">♫</div>';
}
