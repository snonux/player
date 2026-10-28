import { t, html, plural, ui, uiCount } from '../i18n.js';
import { albumCollections, personalCollections, discoverAlbums, mountAlbumDiscovery } from '../music-discovery.js';
import { mountMusicArtwork } from '../music-artwork.js';
import { mountMusicEnrichment } from '../music-enrichment.js';
import { mountRelatedMusic } from '../music-related.js';
import { mountArtistTopSongs } from '../music-top-songs.js';
import { listeningDetails } from '../music-listening.js';
import { ratingControl, bindRatings, ratingFilters, ratingParams } from '../music-ratings.js';
import { favoriteButton, bindFavorites, favoriteFilter } from '../music-favorites.js';
import { readMusicLayout, writeMusicLayout, sortMusic, musicColumns, musicColumnText, musicLayoutToolbar, bindMusicLayout } from '../music-layout.js';
import { mountMusicFacets, facetParams, hasFacets } from '../music-facets.js';
import { renderMusicSearch } from '../music-search.js';
import { collectionButtons, bindCollectionActions, bindCollectionCatalog, cancelCollectionSelection } from '../collection-actions.js';
import { initPlaylists, renderPlaylists } from './music-playlists.js';
import { songActionButtons, bindSongActions, openSongActions, selectionActions } from '../queue-actions.js';
import { cancelPendingSearch } from '../search.js';
import { updateSetRowsUI } from './sets.js';
import { dismissSidebarAfterNav } from './help.js';
import { API } from '../api.js';
import { state } from '../state.js';
import { escapeHtml as esc, fmtDur, toast } from '../utils.js';
import { mediaTitle, artistRoles, albumDiscLayout, albumSongNumber, discTitle } from '../music.js';
import { selectByElement, focusSelectedCard } from '../selection.js';
import { playAlbumCollection } from './playback-nav.js';
import { libraryQueryParams, mountMusicLibraryFilter } from '../music-libraries.js';

let reload = () => {};
let selectedAlbum = '';
let revision = 0;
const positions = new Map();
const history = [];
const viewKey = view => JSON.stringify([view?.artists || false,view?.artist || '',view?.album || '',view?.role || 'artists',view?.playlists || false,view?.playlist || '',view?.search || false,view?.query || '',view?.artistQuery || '',view?.albumQuery || '',view?.facets || {},view?.favorites || false,view?.rated || false,view?.ratingSort || '',view?.collection || '']);
function remember() {
 if (state.music?.search) { state.music.selected={...document.querySelector('#media-grid .selected')?.dataset}; state.music.scroll=window.scrollY; }
 if (!state.music) return;
 const selected = document.querySelector('#media-grid .selected');
 positions.set(viewKey(state.music), {album:selected?.dataset.album, artist:selected?.dataset.artistCard, song:selected?.dataset.id, playlist:selected?.dataset.playlist, scroll:window.scrollY});
}
function restoreSavedScroll(grid, saved, current) {
 if (!saved) return;
 const scroll = saved.scroll;
 let observer, timeout, stopped=false;
 const stop = () => { stopped=true; observer?.disconnect(); clearTimeout(timeout); for (const event of ['wheel','touchstart','pointerdown','keydown']) window.removeEventListener(event,stop); };
 const restore = () => {
  if (stopped) return;
  if (!current()) { stop(); return; }
  window.scrollTo(0,scroll);
 };
 // Artwork and discovery sections may grow after the cards first render.
 observer = new ResizeObserver(() => requestAnimationFrame(restore));
 observer.observe(grid);
 timeout = setTimeout(stop,3000);
 for (const event of ['wheel','touchstart','pointerdown','keydown']) window.addEventListener(event,stop,{once:true});
 restore();
}
function descend(next) { remember(); history.push({...state.music}); state.music=next; reload(); }
export function openArtists() { openAlbums('', '', true); }
function openDiscoveryCollection(collection, items) { descend({...state.music,collection,collectionKeys:items.map(a=>a.album_key)}); }
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
 state.virtualSet = ''; state.podcastFeedKey = ''; state.selectedSetId = null; state.selectedSetIds = []; state.folderPath = '';
 dismissSidebarAfterNav();
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
 if (history.length) { state.music=history.pop(); delete state.music.focusFacet; }
 else if (state.music.playlist) state.music.playlist='';
 else if (state.music.album) state.music.album = '';
 else if (state.music.collection) { delete state.music.collection; delete state.music.collectionKeys; }
 else if (state.music.artist) { state.music.artist = ''; state.music.name = ''; }
 else { state.music = null; }
 reload();
 return true;
}
export async function openSongAlbum(id) {
 try {
  const albums = await API.albums({media_id:id});
  if (!albums.length) { toast(t('No album for this item.'), 'info'); return; }
  openAlbums(); openAlbum(albums[0].album_key);
 } catch (e) { toast(e.message || t('Could not open album.'), 'error'); }
}
export async function renderMusic(grid, isCurrent) {
 const operation = ++revision;
 const view = {...state.music};
 const current = () => operation===revision && isCurrent() && state.music && viewKey(state.music)===viewKey(view);
 state.media = [];
 document.getElementById('breadcrumb-bar')?.classList.add('hidden');
 const count = document.getElementById('result-count');
 count.innerHTML = ui(view.search ? 'Music · Search' : view.playlists ? 'Music · Playlists' : view.artists && !view.album ? 'Music · Artists' : 'Music · Albums');
 grid.classList.toggle('music-detail', Boolean(view.album));
 grid.classList.remove('music-table');
 grid.innerHTML = html`<p class="grid-full" role="status">${ui(view.search ? 'Loading search results…' : view.playlists ? 'Loading playlists…' : view.artists && !view.album ? 'Loading artists…' : 'Loading albums…')}</p>`;
 try {
  await mountMusicFacets(grid,view,current,reload,true);
  if(!current())return;
  if (view.search) {
   await renderMusicSearch(grid,view,current,{openAlbum,openArtist,back:backMusic,reload});
   await mountMusicFacets(grid,view,current,reload);
   favoriteFilter(grid,view,reload); ratingFilters(grid,view,reload);
   return;
  }
  if (view.playlists) { await renderPlaylists(grid,view,current,positions.get(viewKey(view)));return; }
  if (view.artists && !view.album) {
   await renderArtists(grid, view, current);
   await mountMusicFacets(grid,view,current,reload);
   // Favorites/ratings after Filters so they land in [data-music-filter-extras].
   if (!view.artist) { favoriteFilter(grid,view,reload); ratingFilters(grid,view,reload); }
   return;
  }
  let data = view.album ? await API.album(view.album) : await API.albums({...ratingParams(view),collection:personalCollections.includes(view.collection)?'':view.collection || '',favorite:view.favorites || false,...facetParams(view.facets),...(view.artist ? {artist:view.artist} : {}),...libraryQueryParams()});
  if (!current()) return;
  if(!view.album && personalCollections.includes(view.collection)) data=discoverAlbums(data,view.collection);
  const layoutPreferences=readMusicLayout('albums');
  // Migrate leftover Rating-order into Sort by Rating and persist the layout preference.
  if(!view.album && view.ratingSort && !view.collection) {
   layoutPreferences.sort='rating'; layoutPreferences.descending=true;
   writeMusicLayout('albums', layoutPreferences);
   delete state.music.ratingSort; view.ratingSort='';
  }
  if(!view.album && !view.collection) data=sortMusic(data,layoutPreferences,'albums');
  if(!view.album && ['random','liked_mix'].includes(view.collection) && !view.collectionKeys) { state.music.collectionKeys=data.map(a=>a.album_key);view.collectionKeys=state.music.collectionKeys; }
  if(!view.album && ['random','liked_mix'].includes(view.collection) && view.collectionKeys) { const ranks=new Map(view.collectionKeys.map((key,i)=>[key,i]));data.sort((a,b)=>(ranks.get(a.album_key) ?? Infinity)-(ranks.get(b.album_key) ?? Infinity)); }
  if (view.album) {
   state.media = data.songs || [];
   const layout = albumDiscLayout(data);
   count.innerHTML = `${ui('Music · Albums')} · ${esc(data.title)}`;
   grid.innerHTML = html`<div class="grid-full music-heading"><button class="btn btn-ghost" data-music-back>← ${ui('Back to albums')}</button><div class="album-header">${cover(data)}<div><h1>${esc(data.title)}</h1>${creditLinks(data.credits, "album_artist", data.artist)}<p class="text-muted">${data.year || ''}${data.from_folder ? ' · From folder' : ''}</p><p>${uiCount(data.song_count, '{count} song', '{count} songs')} · ${fmtDur(data.duration)}</p>${listeningDetails(data)}<div class="playlist-actions">${collectionButtons('Play album')}<button type="button" class="icon-btn" data-album-more aria-label="More" aria-haspopup="menu" title="More">☰</button><span hidden>${favoriteButton('album',data.album_key,data)}${ratingControl('album',data.album_key,data)}</span></div><div class="music-selection" data-selection-toolbar hidden><span data-selection-count>0 selected</span><button class="btn" data-selection-actions disabled>Selected song actions</button><button class="btn btn-ghost" data-selection-clear>Clear selection</button></div></div></div></div>` + state.media.map((song,index) => html`${layout.grouped && layout.heads.has(song.id) ? discHeading(layout.heads.get(song.id)) : ''}<div class="media-row" role="group" tabindex="0" data-id="${song.id}" data-index="${index}" aria-label="Play ${esc(mediaTitle(song))}"><input type="checkbox" data-song-check="${song.id}" aria-label="Select ${esc(mediaTitle(song))}"><span class="text-muted music-number">${esc(albumSongNumber(song,layout.multi))}</span><span class="music-song"><button class="btn btn-ghost song-open" title="${esc(mediaTitle(song))}">${esc(mediaTitle(song))}</button><span class="text-muted text-sm">${creditLinks(data.song_credits?.[song.id], "track_artist", song.music?.artist || t("Unknown artist"))}</span>${listeningDetails(song)}</span><span>${fmtDur(song.duration)}</span>${compactSongRating(song)}${songActionButtons(song)}<button class="btn btn-ghost" data-song-menu aria-label="Actions for ${esc(mediaTitle(song))}" aria-haspopup="menu" title="More">☰</button><button hidden data-action="song-info" data-song-info="${song.id}" aria-label="Info for ${esc(mediaTitle(song))}">Info</button></div>`).join('');
   mountMusicEnrichment(grid.querySelector('.music-heading'),'album',view.album,current);
   mountRelatedMusic(grid,'album',view.album,current,item=>openAlbum(item.key));
   bindSongActions(grid);
   grid.querySelectorAll('.music-disc img').forEach(img=>img.addEventListener('error',()=>{ const replacement=document.createElement('div'); replacement.className='album-cover album-placeholder'; replacement.setAttribute('role','img'); replacement.setAttribute('aria-label',t('No disc artwork')); replacement.textContent='♫'; img.replaceWith(replacement); },{once:true}));
   grid.querySelectorAll('[data-song-menu]').forEach(button => button.onclick = () => openSongActions(button.closest('.media-row')));
   bindCompactSongRatings(grid);
   const checkedSongs = () => { const ids = [...grid.querySelectorAll('[data-song-check]:checked')].map(input => Number(input.dataset.songCheck)); return state.media.filter(song => ids.includes(song.id)); };
   const updateSelection = () => {
    const count = checkedSongs().length;
    grid.querySelector('[data-selection-toolbar]').hidden = !count;
    grid.querySelector('[data-selection-count]').innerHTML = ui('{count} selected', {count});
    grid.querySelector('[data-selection-actions]').disabled = !count;
   };
   grid.querySelectorAll('[data-song-check]').forEach(input => input.onchange = updateSelection);
   grid.querySelector('[data-selection-clear]').onclick = () => { grid.querySelectorAll('[data-song-check]').forEach(input => input.checked = false); updateSelection(); };
   grid.querySelector('[data-selection-actions]').onclick = e => selectionActions(e.currentTarget, checkedSongs());
   bindCollectionActions(grid.querySelector('[data-collection-actions]'), () => [{kind:'album',key:view.album}], current, data.title);
   grid.querySelector('[data-collection-shuffle]').hidden = true;
   grid.querySelector('[data-collection-menu]').hidden = true;
   grid.querySelectorAll('[data-collection-play]').forEach(button => button.disabled=!data.songs.length);
   grid.querySelector('[data-album-more]').onclick = event => albumDetailMore(event.currentTarget, grid, data);
   bindCredits(grid);
   grid.querySelectorAll('.media-row').forEach(row => {
    row.addEventListener('click', e => { if (e.target.closest('[data-song-info], [data-song-menu], [data-artist], input, [data-action]')) return; selectByElement(row); playAlbumCollection(data.songs,Number(row.dataset.index),data.title); });
   });
   grid.querySelectorAll('[data-song-info]').forEach(button => button.onclick = () => { selectByElement(button.closest('.media-row')); document.dispatchEvent(new Event('music:song-info')); });
  } else {
   count.innerHTML = view.collection ? `${ui('Music')} · ${esc(t(albumCollections[view.collection]?.[0] || 'Albums'))} · ${uiCount(data.length, '{count} album', '{count} albums')}` : view.artist ? ui('Albums by {name}', {name: view.name || t('Artist')}) : `${ui('Music')} · ${uiCount(data.length, '{count} album', '{count} albums')}`;
   grid.innerHTML = html`<div class="grid-full"><button class="btn btn-ghost" data-music-back>← ${view.collection || view.artist ? ui('Back to albums') : ui('Back to Sets')}</button>${view.collection ? html`<h1>${albumCollections[view.collection]?.[0] || t('Albums')}</h1><p class="text-muted">${albumCollections[view.collection]?.[1] || ''}</p>` : ''}${view.artist ? html`<h1>Albums by ${esc(view.name || 'artist')}</h1>` : ''}</div>` + (data.length ? data.map(album => html`<div class="${layoutPreferences.view==='table'?'media-row':'media-card'} album-card" role="button" tabindex="0" data-album="${esc(album.album_key)}" aria-label="Open album ${esc(album.title)} by ${esc(album.artist)}">${layoutPreferences.view==='grid'?cover(album):''}<div class="card-body">${favoriteButton('album',album.album_key,album)} ${compactAlbumRating(album)}<button type="button" class="icon-btn" data-album-card-more aria-label="More" aria-haspopup="menu" title="More">☰</button><div class="title" title="${esc(album.title)}">${esc(album.title)}</div>${layoutPreferences.columns.map(column=>html`<span class="music-column text-muted text-sm" data-column="${column}"><span class="music-column-label">${ui(musicColumns.albums[column])}: </span>${esc(musicColumnText(album,column,'albums'))}</span>`).join('')}${hasFacets(view) ? html`<span class="text-muted text-sm">${album.song_count} matching songs</span>` : ''}${listeningDetails(album)}${album.from_folder ? html`<span class="text-muted text-xs">From folder</span>` : ''}</div></div>`).join('') : html`<p class="grid-full">${view.collection?albumCollections[view.collection]?.[2]:view.favorites?ui('No favorite albums. Show all albums to favorite one.'):ui('Nothing matches these filters. Clear filters or browse Sets for more music.')}</p>`);
   grid.classList.toggle('music-table',layoutPreferences.view==='table');
   grid.querySelector('.grid-full').insertAdjacentHTML('beforeend',musicLayoutToolbar('albums',layoutPreferences));
   grid.querySelectorAll('[data-album]').forEach(card => card.onclick = event => {
    // Open album (.collection-open) must still open; suppress only nested chrome.
    if (event.target.closest('.collection-open')) {
     openAlbum(card.dataset.album);
     return;
    }
    if (event.target.closest('button, input, select, label, [data-album-card-more], .album-rating-badge, [data-rating-label]')) return;
    openAlbum(card.dataset.album);
   });
   grid.querySelectorAll('[data-album-card-more]').forEach(button => button.onclick = event => {
    event.stopPropagation();
    albumCardMore(event.currentTarget, event.currentTarget.closest('.album-card'));
   });
   grid.querySelectorAll('[data-album-rating-open]').forEach(badge => badge.onclick = event => {
    event.stopPropagation();
    openAlbumRatingEditor(badge, badge);
   });
   if(view.collection) {
    const actions=document.createElement('div');actions.innerHTML=collectionButtons('Play collection');grid.querySelector('.grid-full').append(actions);
    bindCollectionActions(actions,()=>data.map(a=>({kind:'album',key:a.album_key})),current,albumCollections[view.collection][0]);
    actions.querySelectorAll('button').forEach(button=>button.disabled=!data.length);
   }
   if(!view.collection && !view.artist && !hasFacets(view) && !view.favorites && !view.rated) mountAlbumDiscovery(grid.querySelector('.grid-full'),data,current,openDiscoveryCollection,openAlbum,state.music,openPlaylist);
   bindCollectionCatalog(grid,data,'album','album_key','title',current);
  }
  bindFavorites(grid,current);bindRatings(grid,current);
  grid.querySelector('[data-music-back]').onclick = backMusic;
  const saved = positions.get(viewKey(view));
  const restored = saved?.album ? grid.querySelector(`[data-album="${CSS.escape(saved.album)}"]`) : saved?.song ? grid.querySelector(`[data-id="${CSS.escape(saved.song)}"]`) : null;
  const target = restored || (!view.album && selectedAlbum ? grid.querySelector(`[data-album="${CSS.escape(selectedAlbum)}"]`) : grid.querySelector('.media-row, .album-card'));
  if (target) { selectByElement(target); focusSelectedCard(); } else grid.querySelector('[data-music-back]')?.focus();
  restoreSavedScroll(grid,saved,current);
  await mountMusicFacets(grid,view,current,reload);
  // Filters extras (favorites/rated/libraries) mount after the disclosure exists.
  if(!view.album){
   favoriteFilter(grid,view,reload);ratingFilters(grid,view,reload);
   await mountMusicLibraryFilter(grid, () => { if (current()) reload(); });
   bindMusicLayout(grid,'albums',layoutPreferences,()=>{delete state.music.ratingSort;reload();},remember);
   if(view.collection) grid.querySelectorAll('[data-layout-sort], [data-layout-direction]').forEach(control=>{control.disabled=true;control.title='This collection uses its own order.';});
  }
 } catch (e) {
  if (!current()) return;
  grid.innerHTML = html`<div class="grid-full" role="alert"><p>${view.search ? t('Could not search music') : t('Could not load')} ${view.search ? '' : view.artists && !view.album ? 'artists' : view.playlists ? 'playlists' : 'albums'}: ${esc(e.message || t('Connection unavailable. Check your connection.'))}</p><button class="btn" data-retry>Retry</button><button class="btn btn-ghost" data-music-back>← Back</button></div>`;
  await mountMusicFacets(grid,view,current,reload);
  grid.querySelector('[data-retry]').onclick = reload;
  grid.querySelector('[data-music-back]').onclick = backMusic;
 }
}
function compactAlbumRating(album) {
 const value = Number(album.rating || 0);
 return html`<button type="button" class="btn btn-ghost album-rating-badge" data-album-rating-open aria-label="${value ? `${value} stars` : 'Unrated'}">${value ? '★'.repeat(value) : '☆'}</button><span hidden>${ratingControl('album', album.album_key, album)}</span>`;
}
function openAlbumRatingEditor(badge, focusBack) {
 const rating = badge.parentElement?.querySelector('[data-music-rating]') || badge.nextElementSibling?.querySelector?.('[data-music-rating]');
 const ratingWrap = badge.nextElementSibling;
 if (!rating || !ratingWrap) return;
 ratingWrap.hidden = false;
 rating.focus();
 const hide = () => {
  ratingWrap.hidden = true;
  const shown = Number(rating.dataset.saved || 0);
  badge.textContent = shown ? '★'.repeat(shown) : '☆';
  badge.setAttribute('aria-label', shown ? `${shown} stars` : 'Unrated');
  if (focusBack?.isConnected) focusBack.focus();
  else if (badge.isConnected) badge.focus();
  rating.removeEventListener('change', onChange);
  rating.removeEventListener('blur', onBlur);
 };
 const onChange = () => {
  // Wait until bindRatings finishes (disabled clears; dataset.saved is authoritative).
  const sync = () => {
   if (rating.disabled) { requestAnimationFrame(sync); return; }
   hide();
  };
  requestAnimationFrame(sync);
 };
 const onBlur = () => setTimeout(() => { if (document.activeElement !== rating) hide(); }, 0);
 rating.addEventListener('change', onChange, { once: true });
 rating.addEventListener('blur', onBlur, { once: true });
}
function albumCardMore(button, card) {
 if (!card) return;
 const badge = card.querySelector('[data-album-rating-open]');
 // Heart stays on the card; More only opens the rating editor (no duplicate Favorite).
 const popover = document.createElement('div');
 popover.className = 'shortcut-picker'; popover.setAttribute('popover', 'auto');
 popover.setAttribute('role', 'menu'); popover.setAttribute('aria-label', t('More album actions'));
 popover.innerHTML = html`<button class="btn btn-ghost" data-more="rating">${esc(t('Album rating'))}</button>`;
 popover.querySelector('[data-more]').onclick = () => {
  popover.hidePopover();
  if (badge) openAlbumRatingEditor(badge, button);
 };
 popover.addEventListener('toggle', event => { if (event.newState === 'closed') { popover.remove(); if (button.isConnected) button.focus(); } });
 document.body.append(popover); popover.showPopover(); popover.querySelector('button')?.focus();
}
function compactSongRating(song) {
 const value = Number(song.rating || 0);
 return html`<button type="button" class="btn btn-ghost song-rating-badge" data-song-rating-open aria-label="${value ? `${value} stars` : 'Unrated'}">${value ? '★'.repeat(value) : '☆'}</button><span hidden>${ratingControl('song', song.id, song)}</span>`;
}
function bindCompactSongRatings(grid) {
 grid.querySelectorAll('[data-song-rating-open]').forEach(badge => {
  badge.onclick = event => {
   event.stopPropagation();
   const select = badge.parentElement?.querySelector('[data-music-rating]') || badge.nextElementSibling?.querySelector?.('[data-music-rating]');
   const wrap = badge.nextElementSibling;
   if (!select || !wrap) return;
   wrap.hidden = false;
   select.focus();
   const hide = () => { wrap.hidden = true; badge.focus(); select.removeEventListener('blur', onBlur); };
   const onBlur = () => setTimeout(() => { if (document.activeElement !== select) hide(); }, 0);
   select.addEventListener('change', () => {
    // Wait until bindRatings finishes (disabled clears; dataset.saved is authoritative).
    const sync = () => {
     if (select.disabled) { requestAnimationFrame(sync); return; }
     const shown = Number(select.dataset.saved || 0);
     badge.textContent = shown ? '★'.repeat(shown) : '☆';
     badge.setAttribute('aria-label', shown ? `${shown} stars` : 'Unrated');
     hide();
    };
    requestAnimationFrame(sync);
   }, { once: true });
   select.addEventListener('blur', onBlur, { once: true });
  };
 });
}
function albumDetailMore(button, grid, data) {
 const favorite = grid.querySelector('[data-entity-favorite]');
 const rating = grid.querySelector('[data-music-rating][data-kind="album"]');
 const ratingWrap = rating?.closest('[hidden], span');
 const collectionMenu = grid.querySelector('[data-collection-menu]');
 const items = [
  ['favorite', favorite?.getAttribute('aria-label') || t('Favorite album')],
  ['rating', t('Album rating')],
  ['shuffle', t('Shuffle')],
  ['collection', t('Queue and offline…')],
 ].filter(([action]) => action !== 'shuffle' && action !== 'collection' || data.songs?.length);
 const popover = document.createElement('div');
 popover.className = 'shortcut-picker'; popover.setAttribute('popover', 'auto');
 popover.setAttribute('role', 'menu'); popover.setAttribute('aria-label', t('More album actions'));
 popover.innerHTML = items.map(([action, label]) => html`<button class="btn btn-ghost" data-more="${action}">${esc(label)}</button>`).join('');
 popover.querySelectorAll('[data-more]').forEach(item => item.onclick = () => {
  popover.hidePopover();
  const action = item.dataset.more;
  if (action === 'favorite') {
   favorite?.click();
   queueMicrotask(() => { if (button.isConnected) button.focus(); });
  } else if (action === 'rating' && rating && ratingWrap) {
   ratingWrap.hidden = false;
   rating.focus();
   const hide = () => { ratingWrap.hidden = true; if (button.isConnected) button.focus(); rating.removeEventListener('change', hide); rating.removeEventListener('blur', onBlur); };
   const onBlur = () => setTimeout(() => { if (document.activeElement !== rating) hide(); }, 0);
   rating.addEventListener('change', hide, { once: true });
   rating.addEventListener('blur', onBlur, { once: true });
  } else if (action === 'shuffle') grid.querySelector('[data-collection-shuffle]')?.click();
  else if (action === 'collection') {
   const restore = event => {
    if (event.target?.matches?.('.shortcut-picker') && event.newState === 'closed') {
     document.removeEventListener('toggle', restore, true);
     queueMicrotask(() => { if (button.isConnected) button.focus(); });
    }
   };
   document.addEventListener('toggle', restore, true);
   collectionMenu?.click();
  }
 });
 popover.addEventListener('toggle', event => { if (event.newState === 'closed') { popover.remove(); if (button.isConnected) button.focus(); } });
 document.body.append(popover); popover.showPopover(); popover.querySelector('button')?.focus();
}
function cover(album) {
 return album.cover_media_id ? html`<img class="album-cover" src="/api/media/${album.cover_media_id}/thumbnail" alt="Artwork for ${esc(album.title)}">` : html`<div class="album-cover album-placeholder" role="img" aria-label="No album artwork">♫</div>`;
}

function creditLinks(credits, role, fallback) {
 const people=(credits || []).filter(person=>person.roles.includes(role));
 return people.length ? people.map(person=>html`<button class="btn btn-ghost credit-link" data-artist="${esc(person.artist_key)}" aria-label="${esc(artistRoles[role])}: ${esc(person.name)}">${esc(person.name)}</button>`).join(' ') : esc(fallback);
}
function bindCredits(grid) { grid.querySelectorAll('[data-artist]').forEach(button=>button.onclick=()=>{ const row=button.closest('.media-row'); if(row) selectByElement(row); openArtist(button.dataset.artist); }); }
async function renderArtists(grid, view, current) {
 const detail=Boolean(view.artist);
 const data=detail ? await API.artist(view.artist) : await API.artists({...ratingParams(view),favorite:view.favorites || false,...facetParams(view.facets),role:view.facets?.role || view.role || 'artists'});
 if (!current()) return;
 document.getElementById('result-count').innerHTML=detail ? `${ui('Music · Artists')} · ${esc(data.name)}` : `${ui('Music')} · ${uiCount(data.length, '{count} artist', '{count} artists')}`;
 const labels=roles=>roles.map(role=>artistRoles[role]).join(' · ');
 const heading=musicArtistHeading(data,view);
 grid.innerHTML=heading+(detail ? data.albums.map(album=>html`<div class="media-card album-card" role="button" tabindex="0" data-album="${esc(album.album_key)}" aria-label="Open album ${esc(album.title)} by ${esc(album.artist)}">${cover(album)}<div class="card-body"><div class="title">${esc(album.title)}</div><div class="text-muted">${esc(album.artist)}</div><div class="text-muted" title="${esc(labels(album.roles))}">${esc(labels(album.roles))}</div>${listeningDetails(album)}</div></div>`).join('') : data.length ? data.map(person=>html`<div class="media-card album-card" role="button" tabindex="0" data-artist-card="${esc(person.artist_key)}" aria-label="Open artist ${esc(person.name)}"><div class="card-body">${favoriteButton('artist',person.artist_key,person)} ${ratingControl('artist',person.artist_key,person)}<div class="title" title="${esc(person.name)}">${esc(person.name)}</div><div class="text-muted" title="${esc(labels(person.roles))}">${esc(labels(person.roles))}</div><div class="text-muted">${uiCount(person.album_count, '{count} album', '{count} albums')} · ${person.song_count} ${hasFacets(view)?'matching songs':'songs'}</div>${listeningDetails(person)}</div></div>`).join('') : html`<p class="grid-full">${view.favorites?ui('No favorite artists. Show all artists to favorite one.'):ui('No artists available for this role. Choose another role or ask an administrator to rescan music tags.')}</p>`);
 bindFavorites(grid,current);bindRatings(grid,current);
 grid.querySelector('[data-music-back]').onclick=backMusic;
 grid.querySelectorAll('[data-album]').forEach(card=>card.onclick=()=>openAlbum(card.dataset.album));
 grid.querySelectorAll('[data-artist-card]').forEach(card=>card.onclick=()=>openArtist(card.dataset.artistCard));
 if (detail) {
  const actions=document.createElement('div'); actions.innerHTML=collectionButtons('Play artist');
  grid.querySelector('.music-heading').append(actions);
  bindCollectionActions(actions,()=>[{kind:'artist',key:view.artist}],current,data.name);
  // Keep Play primary; Shuffle + collection More stay reachable via artist More.
  actions.querySelector('[data-collection-shuffle]')?.setAttribute('hidden','');
  actions.querySelector('[data-collection-menu]')?.setAttribute('hidden','');
 }
 if(detail) mountMusicArtwork(grid.querySelector('.music-heading'),'artist',view.artist,current,data.albums?.find(a=>a.cover_media_id)?cover(data.albums.find(a=>a.cover_media_id)):'',{menuOnly:true});
 if(detail) mountMusicEnrichment(grid.querySelector('.music-heading'),'artist',view.artist,current);
 if(detail) {
  const top=mountArtistTopSongs(grid,data.top_songs,data.name,current,{hidePlayAll:true});
  grid.querySelector('.music-heading').after(top);
  grid.querySelector('[data-artist-more]').onclick = event => artistDetailMore(event.currentTarget, grid, data);
 }
 if(detail) mountRelatedMusic(grid,'artist',view.artist,current,item=>openArtist(item.key));
 bindCollectionCatalog(grid,detail?data.albums:data,detail?'album':'artist',detail?'album_key':'artist_key',detail?'title':'name',current);
 const select=grid.querySelector('select[aria-label="Role"]');
 if (select) select.onchange=()=>{ remember(); state.music.role=select.value; state.music.facets={...state.music.facets,role:select.value==='artists'?'':select.value};reload(); };
 const saved=positions.get(viewKey(view));
 const target=(saved?.artist && grid.querySelector(`[data-artist-card="${CSS.escape(saved.artist)}"]`)) || (saved?.album && grid.querySelector(`[data-album="${CSS.escape(saved.album)}"]`)) || grid.querySelector('.media-card');
 if (target) { selectByElement(target); focusSelectedCard(); } else grid.querySelector('select, [data-music-back]')?.focus();
 restoreSavedScroll(grid,saved,current);
}

function discHeading(disc) {
 const title=discTitle(disc);
 return html`<section class="music-disc" aria-label="${esc(title)}">${cover({title,cover_media_id:disc.cover_media_id})}<h2>${esc(title)}</h2></section>`;
}

function artistDetailMore(button, grid, data) {
 const favorite = grid.querySelector('[data-entity-favorite]');
 const rating = grid.querySelector('[data-music-rating][data-kind="artist"]');
 const ratingHost = grid.querySelector('[data-artist-rating-host]');
 const collectionMenu = grid.querySelector('[data-collection-menu]');
 const shuffle = grid.querySelector('[data-collection-shuffle]');
 const topPlay = grid.querySelector('[data-top-play="0"]');
 const artChange = grid.querySelector('[data-art-change]');
 const artRemove = grid.querySelector('[data-art-remove]');
 const items = [
  ['favorite', favorite?.getAttribute('aria-label') || t('Favorite artist')],
  ['rating', t('Artist rating')],
  ['shuffle', t('Shuffle')],
  ['collection', t('Queue and offline…')],
  ['top', t('Play top songs')],
  ['art-change', t('Change artwork…')],
  ['art-remove', t('Remove artwork')],
 ].filter(([action]) => {
  if (action === 'shuffle' || action === 'collection') return Boolean(data.albums?.length);
  if (action === 'top') return Boolean(topPlay);
  if (action === 'art-change') return Boolean(artChange);
  if (action === 'art-remove') return Boolean(artRemove) && !artRemove.hidden;
  return true;
 });
 const popover = document.createElement('div');
 popover.className = 'shortcut-picker'; popover.setAttribute('popover', 'auto');
 popover.setAttribute('role', 'menu'); popover.setAttribute('aria-label', t('More artist actions'));
 popover.innerHTML = items.map(([action, label]) => html`<button class="btn btn-ghost" data-more="${action}">${esc(label)}</button>`).join('');
 popover.querySelectorAll('[data-more]').forEach(item => item.onclick = () => {
  popover.hidePopover();
  const action = item.dataset.more;
  if (action === 'favorite') {
   favorite?.click();
   queueMicrotask(() => { if (button.isConnected) button.focus(); });
  } else if (action === 'rating' && rating && ratingHost) {
   ratingHost.hidden = false;
   rating.focus();
   const hide = () => { ratingHost.hidden = true; if (button.isConnected) button.focus(); rating.removeEventListener('change', hide); rating.removeEventListener('blur', onBlur); };
   const onBlur = () => setTimeout(() => { if (document.activeElement !== rating) hide(); }, 0);
   rating.addEventListener('change', hide, { once: true });
   rating.addEventListener('blur', onBlur, { once: true });
  } else if (action === 'shuffle') shuffle?.click();
  else if (action === 'collection') {
   const restore = event => {
    if (event.target?.matches?.('.shortcut-picker') && event.newState === 'closed') {
     document.removeEventListener('toggle', restore, true);
     queueMicrotask(() => { if (button.isConnected) button.focus(); });
    }
   };
   document.addEventListener('toggle', restore, true);
   collectionMenu?.click();
  } else if (action === 'top') topPlay?.click();
  else if (action === 'art-change') artChange?.click();
  else if (action === 'art-remove') artRemove?.click();
 });
 popover.addEventListener('toggle', event => { if (event.newState === 'closed') { popover.remove(); if (button.isConnected) button.focus(); } });
 document.body.append(popover); popover.showPopover(); popover.querySelector('button')?.focus();
}

export function musicArtistHeading(data, view) {
 const detail=Boolean(view.artist);
 const labels=roles=>roles.map(role=>t(artistRoles[role])).join(' · ');
 // Detail: Favorite/rating stay in DOM (hidden) for More menu; chrome is Back + Play + More.
 return html`<div class="grid-full music-heading"><button class="btn btn-ghost" data-music-back>← ${detail?ui('Back'):ui('Back to Sets')}</button><h1>${detail?esc(data.name):ui('Artists')}</h1>${detail ? html`<span hidden data-artist-favorite-host>${favoriteButton('artist',data.artist_key,data)}</span><span hidden data-artist-rating-host>${ratingControl('artist',data.artist_key,data)}</span><button type="button" class="icon-btn" data-artist-more aria-label="More" aria-haspopup="menu" title="More">☰</button><p>${esc(labels(data.roles))}</p><p>${uiCount(data.album_count, '{count} album', '{count} albums')} · ${uiCount(data.song_count, '{count} song', '{count} songs')}</p>${listeningDetails(data)}` : html`<label>Role <select aria-label="Role">${Object.entries(artistRoles).map(([key,label])=>html`<option value="${key}" data-i18n="${esc(label)}" ${key===(view.role || 'artists')?'selected':''}>${t(label)}</option>`).join('')}</select></label>`}</div>`;
}
