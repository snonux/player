import { mountMusicArtwork } from '../music-artwork.js';
import { mountMusicEnrichment } from '../music-enrichment.js';
import { mountRelatedMusic } from '../music-related.js';
import { mountArtistTopSongs } from '../music-top-songs.js';
import { listeningDetails } from '../music-listening.js';
import { ratingControl, bindRatings, ratingFilters, ratingParams } from '../music-ratings.js';
import { favoriteButton, bindFavorites, favoriteFilter } from '../music-favorites.js';
import { readMusicLayout, sortMusic, musicColumns, musicColumnText, musicLayoutToolbar, bindMusicLayout } from '../music-layout.js';
import { mountMusicFacets, facetParams, hasFacets } from '../music-facets.js';
import { renderMusicSearch } from '../music-search.js';
import { collectionButtons, bindCollectionActions, bindCollectionCatalog, cancelCollectionSelection } from '../collection-actions.js';
import { initPlaylists, renderPlaylists } from './music-playlists.js';
import { songActionButtons, bindSongActions, openSongActions, selectionActions } from '../queue-actions.js';
import { cancelPendingSearch } from '../search.js';
import { updateSetRowsUI } from './sets.js';
import { API } from '../api.js';
import { state } from '../state.js';
import { escapeHtml as esc, fmtDur, toast } from '../utils.js';
import { mediaTitle, artistRoles, albumDiscLayout, albumSongNumber, discTitle } from '../music.js';
import { selectByElement, focusSelectedCard } from '../selection.js';
import { playAlbumCollection } from './playback-nav.js';

let reload = () => {};
let selectedAlbum = '';
let revision = 0;
const positions = new Map();
const history = [];
const viewKey = view => JSON.stringify([view?.artists || false,view?.artist || '',view?.album || '',view?.role || 'artists',view?.playlists || false,view?.playlist || '',view?.search || false,view?.query || '',view?.artistQuery || '',view?.albumQuery || '',view?.facets || {},view?.favorites || false,view?.rated || false,view?.ratingSort || '']);
function remember() {
 if (state.music?.search) { state.music.selected={...document.querySelector('#media-grid .selected')?.dataset}; state.music.scroll=window.scrollY; }
 if (!state.music) return;
 const selected = document.querySelector('#media-grid .selected');
 positions.set(viewKey(state.music), {album:selected?.dataset.album, artist:selected?.dataset.artistCard, song:selected?.dataset.id, playlist:selected?.dataset.playlist, scroll:window.scrollY});
}
function descend(next) { remember(); history.push({...state.music}); state.music=next; reload(); }
export function openArtists() { openAlbums('', '', true); }
function openArtist(key) { descend({artists:true,artist:key,album:'',role:state.music?.role || 'artists'}); }
export function initMusicCatalog(load) {
 reload = load;
 document.addEventListener('music:rating-refresh',()=>{remember();reload();});
 document.addEventListener('music:favorite-refresh',()=>{remember();reload();});
 document.addEventListener('music:favorite-filter',e=>{if(state.music)state.music.favorites=e.detail;});
 initPlaylists(load,openPlaylist,backMusic);
 document.getElementById('music-playlists')?.addEventListener('click',()=>openAlbums('', '', false, true));
 document.getElementById('music-artists')?.addEventListener('click', openArtists);
 document.getElementById('music-albums')?.addEventListener('click', () => openAlbums());
 document.addEventListener('navigation:restore', () => { history.length=0; });
 document.addEventListener('search:typing', () => { revision++; });
 document.addEventListener('music:album', e => openAlbum(e.detail));
}
export function openAlbums(artist = '', name = '', artists = false, playlists = false) {
 document.dispatchEvent(new Event('search:forget'));
 cancelPendingSearch();
 history.length=0; positions.clear();
 state.music = playlists ? {playlists:true,playlist:''} : artists ? {artists:true,artist:'',album:'',role:'artists'} : { artist, name, album: '' };
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
 descend(state.music.search ? {album:key,artist:'',name:''} : {...state.music,album:key});
}
export function openPlaylist(id) { descend({playlists:true,playlist:id}); }
export function backMusic() {
 if (cancelCollectionSelection()) return true;
 if (!state.music) return false;
 if(state.music.search) { document.dispatchEvent(new Event('search:restore')); return true; }
 remember();
 if (history.length) state.music=history.pop();
 else if (state.music.playlist) state.music.playlist='';
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
 count.textContent = view.search ? 'Music · Search' : view.playlists ? 'Music · Playlists' : view.artists && !view.album ? 'Music · Artists' : 'Music · Albums';
 grid.classList.toggle('music-detail', Boolean(view.album));
 grid.classList.remove('music-table');
 grid.innerHTML = `<p class="grid-full" role="status">Loading ${view.search ? 'search results' : view.playlists ? 'playlists' : view.artists && !view.album ? 'artists' : 'albums'}…</p>`;
 try {
  await mountMusicFacets(grid,view,current,reload,true);
  if(!current())return;
  if (view.search) { await renderMusicSearch(grid,view,current,{openAlbum,openArtist,back:backMusic,reload});await mountMusicFacets(grid,view,current,reload);return; }
  if (view.playlists) { await renderPlaylists(grid,view,current,positions.get(viewKey(view)));return; }
  if (view.artists && !view.album) { await renderArtists(grid, view, current);await mountMusicFacets(grid,view,current,reload); return; }
  let data = view.album ? await API.album(view.album) : await API.albums({...ratingParams(view),favorite:view.favorites || false,...facetParams(view.facets),...(view.artist ? {artist:view.artist} : {})});
  if (!current()) return;
  const layoutPreferences=readMusicLayout('albums');
  if(!view.album && !view.ratingSort) data=sortMusic(data,layoutPreferences,'albums');
  if (view.album) {
   state.media = data.songs || [];
   const layout = albumDiscLayout(data);
   count.textContent = `Music · Albums · ${data.title}`;
   grid.innerHTML = `<div class="grid-full music-heading"><button class="btn btn-ghost" data-music-back>← Back to albums</button><div class="album-header">${cover(data)}<div><h1>${esc(data.title)}</h1>${creditLinks(data.credits, "album_artist", data.artist)}<p class="text-muted">${data.year || ''}${data.from_folder ? ' · From folder' : ''}</p><p>${data.song_count} songs · ${fmtDur(data.duration)}</p>${listeningDetails(data)}${favoriteButton('album',data.album_key,data)} ${ratingControl('album',data.album_key,data)}${collectionButtons('Play album')}<div class="music-selection"><span data-selection-count>0 selected</span><button class="btn" data-selection-actions disabled>Selected song actions</button><button class="btn btn-ghost" data-selection-clear>Clear selection</button></div></div></div></div>` + state.media.map((song,index) => `${layout.grouped && layout.heads.has(song.id) ? discHeading(layout.heads.get(song.id)) : ''}<div class="media-row" role="group" tabindex="0" data-id="${song.id}" data-index="${index}" aria-label="Play ${esc(mediaTitle(song))}"><input type="checkbox" data-song-check="${song.id}" aria-label="Select ${esc(mediaTitle(song))}"><span class="text-muted music-number">${esc(albumSongNumber(song,layout.multi))}</span><span class="music-song"><button class="btn btn-ghost song-open" title="${esc(mediaTitle(song))}">${esc(mediaTitle(song))}</button><span class="text-muted text-sm">${creditLinks(data.song_credits?.[song.id], "track_artist", song.music?.artist || "Unknown artist")}</span>${listeningDetails(song)}</span><span>${fmtDur(song.duration)}</span>${ratingControl('song',song.id,song)}${songActionButtons(song)}<button class="btn btn-ghost" data-song-menu aria-label="Actions for ${esc(mediaTitle(song))}">⋮</button><button hidden data-action="song-info" data-song-info="${song.id}" aria-label="Info for ${esc(mediaTitle(song))}">Info</button></div>`).join('');
   mountMusicEnrichment(grid.querySelector('.music-heading'),'album',view.album,current);
   mountRelatedMusic(grid,'album',view.album,current,item=>openAlbum(item.key));
   bindSongActions(grid);
   grid.querySelectorAll('.music-disc img').forEach(img=>img.addEventListener('error',()=>{ const replacement=document.createElement('div'); replacement.className='album-cover album-placeholder'; replacement.setAttribute('role','img'); replacement.setAttribute('aria-label','No disc artwork'); replacement.textContent='♫'; img.replaceWith(replacement); },{once:true}));
   grid.querySelectorAll('[data-song-menu]').forEach(button => button.onclick = () => openSongActions(button.closest('.media-row')));
   const checkedSongs = () => { const ids = [...grid.querySelectorAll('[data-song-check]:checked')].map(input => Number(input.dataset.songCheck)); return state.media.filter(song => ids.includes(song.id)); };
   const updateSelection = () => { const count = checkedSongs().length; grid.querySelector('[data-selection-count]').textContent = `${count} selected`; grid.querySelector('[data-selection-actions]').disabled = !count; };
   grid.querySelectorAll('[data-song-check]').forEach(input => input.onchange = updateSelection);
   grid.querySelector('[data-selection-clear]').onclick = () => { grid.querySelectorAll('[data-song-check]').forEach(input => input.checked = false); updateSelection(); };
   grid.querySelector('[data-selection-actions]').onclick = e => selectionActions(e.currentTarget, checkedSongs());
   bindCollectionActions(grid.querySelector('[data-collection-actions]'), () => [{kind:'album',key:view.album}], current, data.title);
   grid.querySelectorAll('[data-collection-play], [data-collection-menu]').forEach(button => button.disabled=!data.songs.length);
   bindCredits(grid);
   grid.querySelectorAll('.media-row').forEach(row => {
    row.addEventListener('click', e => { if (e.target.closest('[data-song-info], [data-song-menu], [data-artist], input, [data-action]')) return; selectByElement(row); playAlbumCollection(data.songs,Number(row.dataset.index),data.title); });
   });
   grid.querySelectorAll('[data-song-info]').forEach(button => button.onclick = () => { selectByElement(button.closest('.media-row')); document.dispatchEvent(new Event('music:song-info')); });
  } else {
   count.textContent = view.artist ? `Albums by ${view.name || 'artist'}` : `Music · ${data.length} albums${hasFacets(view)?' matching filters':''}`;
   grid.innerHTML = `<div class="grid-full"><button class="btn btn-ghost" data-music-back>← ${view.artist ? 'Back to albums' : 'Back to Sets'}</button>${view.artist ? `<h1>Albums by ${esc(view.name || 'artist')}</h1>` : ''}</div>` + (data.length ? data.map(album => `<div class="${layoutPreferences.view==='table'?'media-row':'media-card'} album-card" role="button" tabindex="0" data-album="${esc(album.album_key)}" aria-label="Open album ${esc(album.title)} by ${esc(album.artist)}">${layoutPreferences.view==='grid'?cover(album):''}<div class="card-body">${favoriteButton('album',album.album_key,album)} ${ratingControl('album',album.album_key,album)}<div class="title" title="${esc(album.title)}">${esc(album.title)}</div>${layoutPreferences.columns.map(column=>`<span class="music-column text-muted text-sm" data-column="${column}"><span class="music-column-label">${musicColumns.albums[column]}: </span>${esc(musicColumnText(album,column,'albums'))}</span>`).join('')}${hasFacets(view) ? `<span class="text-muted text-sm">${album.song_count} matching songs</span>` : ''}${listeningDetails(album)}${album.from_folder ? '<span class="text-muted text-xs">From folder</span>' : ''}</div></div>`).join('') : `<p class="grid-full">${view.favorites?'No favorite albums. Show all albums to favorite one.':'Nothing matches these filters. Clear filters or browse Sets for more music.'}</p>`);
   grid.classList.toggle('music-table',layoutPreferences.view==='table');
   grid.querySelector('.grid-full').insertAdjacentHTML('beforeend',musicLayoutToolbar('albums',layoutPreferences));
   grid.querySelectorAll('[data-album]').forEach(card => card.onclick = () => openAlbum(card.dataset.album));
   bindCollectionCatalog(grid,data,'album','album_key','title',current);
  }
  bindFavorites(grid,current);bindRatings(grid,current);
  if(!view.album){favoriteFilter(grid,view,reload);ratingFilters(grid,view,reload);}
  grid.querySelector('[data-music-back]').onclick = backMusic;
  const saved = positions.get(viewKey(view));
  const restored = saved?.album ? grid.querySelector(`[data-album="${CSS.escape(saved.album)}"]`) : saved?.song ? grid.querySelector(`[data-id="${CSS.escape(saved.song)}"]`) : null;
  const target = restored || (!view.album && selectedAlbum ? grid.querySelector(`[data-album="${CSS.escape(selectedAlbum)}"]`) : grid.querySelector('.media-row, .album-card'));
  if (target) { selectByElement(target); focusSelectedCard(); } else grid.querySelector('[data-music-back]')?.focus();
  if (saved) window.scrollTo(0,saved.scroll);
  await mountMusicFacets(grid,view,current,reload);
  if(!view.album) bindMusicLayout(grid,'albums',layoutPreferences,reload,remember);
 } catch (e) {
  if (!current()) return;
  grid.innerHTML = `<div class="grid-full" role="alert"><p>${view.search ? 'Could not search music' : 'Could not load'} ${view.search ? '' : view.artists && !view.album ? 'artists' : view.playlists ? 'playlists' : 'albums'}: ${esc(e.message || 'Connection unavailable. Check your connection.')}</p><button class="btn" data-retry>Retry</button><button class="btn btn-ghost" data-music-back>← Back</button></div>`;
  await mountMusicFacets(grid,view,current,reload);
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
function bindCredits(grid) { grid.querySelectorAll('[data-artist]').forEach(button=>button.onclick=()=>{ const row=button.closest('.media-row'); if(row) selectByElement(row); openArtist(button.dataset.artist); }); }
async function renderArtists(grid, view, current) {
 const detail=Boolean(view.artist);
 const data=detail ? await API.artist(view.artist) : await API.artists({...ratingParams(view),favorite:view.favorites || false,...facetParams(view.facets),role:view.facets?.role || view.role || 'artists'});
 if (!current()) return;
 document.getElementById('result-count').textContent=detail ? `Music · Artists · ${data.name}` : `Music · ${data.length} artists`;
 const labels=roles=>roles.map(role=>artistRoles[role]).join(' · ');
 const heading=`<div class="grid-full music-heading"><button class="btn btn-ghost" data-music-back>← ${detail?'Back':'Back to Sets'}</button><h1>${detail?esc(data.name):'Artists'}</h1>${detail ? `${favoriteButton('artist',data.artist_key,data)} ${ratingControl('artist',data.artist_key,data)}<p>${esc(labels(data.roles))}</p><p>${data.album_count} albums · ${data.song_count} songs</p>${listeningDetails(data)}` : `<label>Role <select aria-label="Role">${Object.entries(artistRoles).map(([key,label])=>`<option value="${key}" ${key===(view.role || 'artists')?'selected':''}>${esc(label)}</option>`).join('')}</select></label>`}</div>`;
 grid.innerHTML=heading+(detail ? data.albums.map(album=>`<div class="media-card album-card" role="button" tabindex="0" data-album="${esc(album.album_key)}" aria-label="Open album ${esc(album.title)} by ${esc(album.artist)}">${cover(album)}<div class="card-body"><div class="title">${esc(album.title)}</div><div class="text-muted">${esc(album.artist)}</div><div class="text-muted" title="${esc(labels(album.roles))}">${esc(labels(album.roles))}</div>${listeningDetails(album)}</div></div>`).join('') : data.length ? data.map(person=>`<div class="media-card album-card" role="button" tabindex="0" data-artist-card="${esc(person.artist_key)}" aria-label="Open artist ${esc(person.name)}"><div class="card-body">${favoriteButton('artist',person.artist_key,person)} ${ratingControl('artist',person.artist_key,person)}<div class="title" title="${esc(person.name)}">${esc(person.name)}</div><div class="text-muted" title="${esc(labels(person.roles))}">${esc(labels(person.roles))}</div><div class="text-muted">${person.album_count} albums · ${person.song_count} ${hasFacets(view)?'matching songs':'songs'}</div>${listeningDetails(person)}</div></div>`).join('') : `<p class="grid-full">${view.favorites?'No favorite artists. Show all artists to favorite one.':'No artists available for this role. Choose another role or ask an administrator to rescan music tags.'}</p>`);
 bindFavorites(grid,current);bindRatings(grid,current);
 if(!detail){favoriteFilter(grid,view,reload);ratingFilters(grid,view,reload);}
 grid.querySelector('[data-music-back]').onclick=backMusic;
 grid.querySelectorAll('[data-album]').forEach(card=>card.onclick=()=>openAlbum(card.dataset.album));
 grid.querySelectorAll('[data-artist-card]').forEach(card=>card.onclick=()=>openArtist(card.dataset.artistCard));
 if (detail) {
  const actions=document.createElement('div'); actions.innerHTML=collectionButtons('Play artist');
  grid.querySelector('.music-heading').append(actions);
  bindCollectionActions(actions,()=>[{kind:'artist',key:view.artist}],current,data.name);
 }
 if(detail) mountMusicArtwork(grid.querySelector('.music-heading'),'artist',view.artist,current,data.albums?.find(a=>a.cover_media_id)?cover(data.albums.find(a=>a.cover_media_id)):'');
 if(detail) mountMusicEnrichment(grid.querySelector('.music-heading'),'artist',view.artist,current);
 if(detail) {
  const top=mountArtistTopSongs(grid,data.top_songs,data.name,current);
  grid.querySelector('.music-heading').after(top);
 }
 if(detail) mountRelatedMusic(grid,'artist',view.artist,current,item=>openArtist(item.key));
 bindCollectionCatalog(grid,detail?data.albums:data,detail?'album':'artist',detail?'album_key':'artist_key',detail?'title':'name',current);
 const select=grid.querySelector('select[aria-label="Role"]');
 if (select) select.onchange=()=>{ remember(); state.music.role=select.value; state.music.facets={...state.music.facets,role:select.value==='artists'?'':select.value};reload(); };
 const saved=positions.get(viewKey(view));
 const target=(saved?.artist && grid.querySelector(`[data-artist-card="${CSS.escape(saved.artist)}"]`)) || (saved?.album && grid.querySelector(`[data-album="${CSS.escape(saved.album)}"]`)) || grid.querySelector('.media-card');
 if (target) { selectByElement(target); focusSelectedCard(); } else grid.querySelector('select, [data-music-back]')?.focus();
 if (saved) window.scrollTo(0,saved.scroll);
}

function discHeading(disc) {
 const title=discTitle(disc);
 return `<section class="music-disc" aria-label="${esc(title)}">${cover({title,cover_media_id:disc.cover_media_id})}<h2>${esc(title)}</h2></section>`;
}
