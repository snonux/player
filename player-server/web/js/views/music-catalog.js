import { songActionButtons, bindSongActions, openSongActions, selectionActions } from '../queue-actions.js';
import { cancelPendingSearch } from '../search.js';
import { updateSetRowsUI } from './sets.js';
import { API } from '../api.js';
import { state } from '../state.js';
import { escapeHtml as esc, fmtDur, toast } from '../utils.js';
import { mediaTitle, artistRoles } from '../music.js';
import { selectByElement, focusSelectedCard } from '../selection.js';
import { playAlbumCollection } from './playback-nav.js';

let reload = () => {};
let selectedAlbum = '';
let revision = 0;
const positions = new Map();
const history = [];
const viewKey = view => JSON.stringify([view?.artists || false,view?.artist || '',view?.album || '',view?.role || 'artists']);
function remember() {
 if (!state.music) return;
 const selected = document.querySelector('#media-grid .selected');
 positions.set(viewKey(state.music), {album:selected?.dataset.album, artist:selected?.dataset.artistCard, song:selected?.dataset.id, scroll:window.scrollY});
}
function descend(next) { remember(); history.push({...state.music}); state.music=next; reload(); }
export function openArtists() { openAlbums('', '', true); }
function openArtist(key) { descend({artists:true,artist:key,album:'',role:state.music?.role || 'artists'}); }
export function initMusicCatalog(load) {
 reload = load;
 document.getElementById('music-artists')?.addEventListener('click', openArtists);
 document.getElementById('music-albums')?.addEventListener('click', () => openAlbums());
 document.addEventListener('navigation:restore', () => { history.length=0; });
 document.addEventListener('music:album', e => openAlbum(e.detail));
}
export function openAlbums(artist = '', name = '', artists = false) {
 cancelPendingSearch();
 history.length=0; positions.clear();
 state.music = artists ? {artists:true,artist:'',album:'',role:'artists'} : { artist, name, album: '' };
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
 descend({...state.music,album:key});
}
export function backMusic() {
 if (!state.music) return false;
 remember();
 if (history.length) state.music=history.pop();
 else if (state.music.album) state.music.album = '';
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
 const current = () => operation===revision && isCurrent() && state.music && viewKey(state.music)===viewKey(view);
 state.media = [];
 document.getElementById('breadcrumb-bar')?.classList.add('hidden');
 const count = document.getElementById('result-count');
 count.textContent = view.artists && !view.album ? 'Music · Artists' : 'Music · Albums';
 grid.classList.toggle('music-detail', Boolean(view.album));
 grid.innerHTML = `<p class="grid-full" role="status">Loading ${view.artists && !view.album ? 'artists' : 'albums'}…</p>`;
 try {
  if (view.artists && !view.album) { await renderArtists(grid, view, current); return; }
  const data = view.album ? await API.album(view.album) : await API.albums(view.artist ? {artist:view.artist} : {});
  if (!current()) return;
  if (view.album) {
   state.media = data.songs || [];
   count.textContent = `Music · Albums · ${data.title}`;
   grid.innerHTML = `<div class="grid-full music-heading"><button class="btn btn-ghost" data-music-back>← Back to albums</button><div class="album-header">${cover(data)}<div><h1>${esc(data.title)}</h1>${creditLinks(data.credits, "album_artist", data.artist)}<p class="text-muted">${data.year || ''}${data.from_folder ? ' · From folder' : ''}</p><p>${data.song_count} songs · ${fmtDur(data.duration)}</p><button class="btn btn-primary" data-album-play>▶ Play album</button><div class="music-selection"><span data-selection-count>0 selected</span><button class="btn" data-selection-actions disabled>Selected song actions</button><button class="btn btn-ghost" data-selection-clear>Clear selection</button></div></div></div></div>` + state.media.map((song,index) => `<div class="media-row" role="group" tabindex="0" data-id="${song.id}" data-index="${index}" aria-label="Play ${esc(mediaTitle(song))}"><input type="checkbox" data-song-check="${song.id}" aria-label="Select ${esc(mediaTitle(song))}"><span class="text-muted music-number">${song.music?.disc ? `${song.music.disc} · ` : ''}${song.music?.track || '—'}</span><span class="music-song"><button class="btn btn-ghost song-open" title="${esc(mediaTitle(song))}">${esc(mediaTitle(song))}</button><span class="text-muted text-sm">${creditLinks(data.song_credits?.[song.id], "track_artist", song.music?.artist || "Unknown artist")}</span></span><span>${fmtDur(song.duration)}</span>${songActionButtons(song)}<button class="btn btn-ghost" data-song-menu aria-label="Actions for ${esc(mediaTitle(song))}">⋮</button><button hidden data-action="song-info" data-song-info="${song.id}" aria-label="Info for ${esc(mediaTitle(song))}">Info</button></div>`).join('');
   bindSongActions(grid);
   grid.querySelectorAll('[data-song-menu]').forEach(button => button.onclick = () => openSongActions(button.closest('.media-row')));
   const checkedSongs = () => { const ids = [...grid.querySelectorAll('[data-song-check]:checked')].map(input => Number(input.dataset.songCheck)); return state.media.filter(song => ids.includes(song.id)); };
   const updateSelection = () => { const count = checkedSongs().length; grid.querySelector('[data-selection-count]').textContent = `${count} selected`; grid.querySelector('[data-selection-actions]').disabled = !count; };
   grid.querySelectorAll('[data-song-check]').forEach(input => input.onchange = updateSelection);
   grid.querySelector('[data-selection-clear]').onclick = () => { grid.querySelectorAll('[data-song-check]').forEach(input => input.checked = false); updateSelection(); };
   grid.querySelector('[data-selection-actions]').onclick = e => selectionActions(e.currentTarget, checkedSongs());
   grid.querySelector('[data-album-play]').onclick = () => playAlbumCollection(data.songs,0,data.title);
   bindCredits(grid);
   grid.querySelectorAll('.media-row').forEach(row => {
    row.addEventListener('click', e => { if (e.target.closest('[data-song-info], [data-song-menu], [data-artist], input, [data-action]')) return; selectByElement(row); playAlbumCollection(data.songs,Number(row.dataset.index),data.title); });
   });
   grid.querySelectorAll('[data-song-info]').forEach(button => button.onclick = () => { selectByElement(button.closest('.media-row')); document.dispatchEvent(new Event('music:song-info')); });
  } else {
   count.textContent = view.artist ? `Albums by ${view.name || 'artist'}` : `Music · ${data.length} albums`;
   grid.innerHTML = `<div class="grid-full"><button class="btn btn-ghost" data-music-back>← ${view.artist ? 'Back to albums' : 'Back to Sets'}</button>${view.artist ? `<h1>Albums by ${esc(view.name || 'artist')}</h1>` : ''}</div>` + (data.length ? data.map(album => `<div class="media-card album-card" role="button" tabindex="0" data-album="${esc(album.album_key)}" aria-label="Open album ${esc(album.title)} by ${esc(album.artist)}">${cover(album)}<div class="card-body"><div class="title" title="${esc(album.title)}">${esc(album.title)}</div><div class="text-muted text-sm">${esc(album.artist)}${album.year ? ` · ${album.year}` : ''}</div>${album.from_folder ? '<span class="text-muted text-xs">From folder</span>' : ''}</div></div>`).join('') : '<p class="grid-full">No albums available. Music includes audio from your permitted sets. Browse Sets or ask an administrator to rescan music tags.</p>');
   grid.querySelectorAll('[data-album]').forEach(card => card.onclick = () => openAlbum(card.dataset.album));
  }
  grid.querySelector('[data-music-back]').onclick = backMusic;
  const saved = positions.get(viewKey(view));
  const restored = saved?.album ? grid.querySelector(`[data-album="${CSS.escape(saved.album)}"]`) : saved?.song ? grid.querySelector(`[data-id="${CSS.escape(saved.song)}"]`) : null;
  const target = restored || (!view.album && selectedAlbum ? grid.querySelector(`[data-album="${CSS.escape(selectedAlbum)}"]`) : grid.querySelector('.media-row, .album-card'));
  if (target) { selectByElement(target); focusSelectedCard(); } else grid.querySelector('[data-music-back]')?.focus();
  if (saved) window.scrollTo(0,saved.scroll);
 } catch (e) {
  if (!current()) return;
  grid.innerHTML = `<div class="grid-full" role="alert"><p>Could not load ${view.artists && !view.album ? 'artists' : 'albums'}: ${esc(e.message || 'Connection unavailable. Check your connection.')}</p><button class="btn" data-retry>Retry</button><button class="btn btn-ghost" data-music-back>← Back</button></div>`;
  grid.querySelector('[data-retry]').onclick = reload;
  grid.querySelector('[data-music-back]').onclick = backMusic;
 }
}
function cover(album) {
 return album.cover_media_id ? `<img class="album-cover" src="/api/media/${album.cover_media_id}/thumbnail" alt="Artwork for ${esc(album.title)}">` : '<div class="album-cover album-placeholder" role="img" aria-label="No album artwork">♫</div>';
}

function creditLinks(credits, role, fallback) {
 const people=(credits || []).filter(person=>person.roles.includes(role));
 return people.length ? people.map(person=>`<button class="btn btn-ghost credit-link" data-artist="${esc(person.artist_key)}" aria-label="${esc(artistRoles[role])}: ${esc(person.name)}">${esc(person.name)}</button>`).join(' ') : esc(fallback);
}
function bindCredits(grid) { grid.querySelectorAll('[data-artist]').forEach(button=>button.onclick=()=>openArtist(button.dataset.artist)); }
async function renderArtists(grid, view, current) {
 const detail=Boolean(view.artist);
 const data=detail ? await API.artist(view.artist) : await API.artists({role:view.role || 'artists'});
 if (!current()) return;
 document.getElementById('result-count').textContent=detail ? `Music · Artists · ${data.name}` : `Music · ${data.length} artists`;
 const labels=roles=>roles.map(role=>artistRoles[role]).join(' · ');
 const heading=`<div class="grid-full music-heading"><button class="btn btn-ghost" data-music-back>← ${detail?'Back':'Back to Sets'}</button><h1>${detail?esc(data.name):'Artists'}</h1>${detail ? `<p>${esc(labels(data.roles))}</p><p>${data.album_count} albums · ${data.song_count} songs</p>` : `<label>Role <select aria-label="Role">${Object.entries(artistRoles).map(([key,label])=>`<option value="${key}" ${key===(view.role || 'artists')?'selected':''}>${esc(label)}</option>`).join('')}</select></label>`}</div>`;
 grid.innerHTML=heading+(detail ? data.albums.map(album=>`<div class="media-card album-card" role="button" tabindex="0" data-album="${esc(album.album_key)}" aria-label="Open album ${esc(album.title)} by ${esc(album.artist)}">${cover(album)}<div class="card-body"><div class="title">${esc(album.title)}</div><div class="text-muted">${esc(album.artist)}</div><div class="text-muted" title="${esc(labels(album.roles))}">${esc(labels(album.roles))}</div></div></div>`).join('') : data.length ? data.map(person=>`<div class="media-card album-card" role="button" tabindex="0" data-artist-card="${esc(person.artist_key)}" aria-label="Open artist ${esc(person.name)}"><div class="card-body"><div class="title" title="${esc(person.name)}">${esc(person.name)}</div><div class="text-muted" title="${esc(labels(person.roles))}">${esc(labels(person.roles))}</div><div class="text-muted">${person.album_count} albums · ${person.song_count} songs</div></div></div>`).join('') : '<p class="grid-full">No artists available for this role. Choose another role or ask an administrator to rescan music tags.</p>');
 grid.querySelector('[data-music-back]').onclick=backMusic;
 grid.querySelectorAll('[data-album]').forEach(card=>card.onclick=()=>openAlbum(card.dataset.album));
 grid.querySelectorAll('[data-artist-card]').forEach(card=>card.onclick=()=>openArtist(card.dataset.artistCard));
 const select=grid.querySelector('select');
 if (select) select.onchange=()=>{ remember(); state.music.role=select.value; reload(); };
 const saved=positions.get(viewKey(view));
 const target=(saved?.artist && grid.querySelector(`[data-artist-card="${CSS.escape(saved.artist)}"]`)) || (saved?.album && grid.querySelector(`[data-album="${CSS.escape(saved.album)}"]`)) || grid.querySelector('.media-card');
 if (target) { selectByElement(target); focusSelectedCard(); } else grid.querySelector('select, [data-music-back]')?.focus();
 if (saved) window.scrollTo(0,saved.scroll);
}
