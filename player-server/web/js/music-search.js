import { readMusicLayout, sortMusic, musicColumns, musicColumnText, musicLayoutToolbar, bindMusicLayout } from './music-layout.js';
import { facetParams, hasFacets } from './music-facets.js';
import { state } from './state.js';
import { API } from './api.js';
import { escapeHtml as esc, fmtDur } from './utils.js';
import { mediaTitle } from './music.js';
import { currentElement, selectByElement, focusSelectedCard } from './selection.js';
import { songActionButtons, bindSongActions, openSongActions } from './queue-actions.js';

let previous = null;
export function musicQuery(raw, parsed, musicActive = false) {
 // Clearing ordinary Sets filters must keep the generic view and set scope.
 if (!raw.trim() && !musicActive) return null;
 const constraints = {query: [], artistQuery: [], albumQuery: []};
 const facets={};
 // Quoted phrases keep spaces, while punctuation in a real name stays literal.
 for (const token of raw.match(/(?:[^\s"]+|"[^"]*")+/gu) || []) {
  const facet=token.match(/^(genre|year):(.+)$/iu);
  if(facet){facets[facet[1].toLowerCase()]=facet[2].replaceAll('"','');continue;}
  const match = token.match(/^(artist|album):(.+)$/iu);
  const value = (match ? match[2] : token).replaceAll('"','');
  constraints[match ? `${match[1].toLowerCase()}Query` : 'query'].push(value);
 }
 const fileOnly = Object.entries(parsed).some(([key,value]) => key !== 'search' && Boolean(value)) || (raw.match(/(?:[^\s"]+|"[^"]*")+/gu) || []).some(token=>/^(?:min|max|tag|like|type|set|sort|minsize|maxsize):.+/iu.test(token));
 return fileOnly ? null : {...Object.fromEntries(Object.entries(constraints).map(([key,parts]) => [key,parts.join(' ').trim()])),...(Object.keys(facets).length?{facets}:{})};
}
// Keep typed modifiers separate from explicit toolbar values, including values
// temporarily shadowed by a modifier for the same dimension.
export function nextMusicSearch(previous, query) {
 const explicitFacets={...(previous?.explicitFacets ?? previous?.facets ?? {})};
 if(!previous?.explicitFacets) for(const key of Object.keys(previous?.queryFacets || {})) delete explicitFacets[key];
 const queryFacets={...query.facets};
 return {search:true,...query,explicitFacets,queryFacets,facets:{...explicitFacets,...queryFacets}};
}
export function captureSearchView() {
 if (previous || state.music?.search) return;
 previous = {music: state.music ? {...state.music} : null, filters:{...state.filters}, selectedSetId:state.selectedSetId, selectedSetIds:[...state.selectedSetIds],virtualSet:state.virtualSet,folderPath:state.folderPath,mediaPage:state.mediaPage,selected: currentElement()?.dataset,scroll:globalThis.scrollY};
 // DOMStringMap is live; retain an independent identity.
 if (previous.selected) previous.selected = {...previous.selected};
}
export async function restoreSearchView(load) {
 if (!previous) return false;
 const saved=previous; previous=null;
 const {selected,scroll,...view}=saved;
 Object.assign(state,view);
 const input=document.getElementById('search-input'); if(input) input.value=saved.filters.search || '';
 await load();
 const rows=[...document.querySelectorAll('#media-grid .media-card, #media-grid .media-row, #media-grid .folder-card, #media-grid .set-card')];
 const target=rows.find(row=>selected && Object.entries(selected).every(([key,value])=>row.dataset[key]===value));
 if(target) {selectByElement(target);focusSelectedCard();}
 globalThis.scrollTo?.(0,scroll);
 return true;
}
export function forgetSearchView() { previous=null; }
export function hasSearchView() { return Boolean(previous); }
export async function renderMusicSearch(grid, view, current, {openAlbum, openArtist, back, reload}) {
 const query=view.query || '', artist=view.artistQuery || '', album=view.albumQuery || '';
 const header=`<div class="grid-full music-heading"><button class="btn btn-ghost" data-search-back>← Back</button><h1>Search results</h1><p>${esc([query,artist && `Artist: ${artist}`,album && `Album: ${album}`].filter(Boolean).join(' · '))}</p></div>`;
 const wire=()=>{grid.querySelector('[data-search-back]')?.addEventListener('click',back);};
 if(!query && !artist && !album && !hasFacets(view)) {grid.innerHTML=header+'<p class="grid-full" role="status">Search songs, albums and artists.</p>';wire();return;}
 const [data, media]=await Promise.all([API.musicSearch({q:query,artist,album,...facetParams(view.facets)}), query && !artist && !album && !hasFacets(view) ? searchFiles(query,current) : []]);
 if(!current()) return;
 const layout=readMusicLayout('songs');
 const songs=sortMusic(data.songs || [],layout,'songs'), ids=new Set(songs.map(song=>song.id));
 const needle=query.toLocaleLowerCase();
 const files=(Array.isArray(media)?media:media?.media || []).filter(item=>!ids.has(item.id) && [item.file_name,item.rel_path].some(text=>(text || '').toLocaleLowerCase().includes(needle)));
 state.searchSongs=songs;state.searchFiles=files;state.media=[...songs,...files];
 const group=(label,items,rows)=>`<h2 class="grid-full">${label} (${items.length})</h2>${rows}`;
 const songRows=songs.map((song,index)=>`<div class="media-row" tabindex="0" role="group" data-id="${song.id}" data-index="${index}" data-search-kind="song" aria-label="Play ${esc(mediaTitle(song))}"><span class="music-song"><button class="btn btn-ghost song-open">${esc(mediaTitle(song))}</button><span class="text-muted text-sm">${esc(layout.columns.filter(column=>column!=='duration').map(column=>`${musicColumns.songs[column]}: ${musicColumnText(song,column,'songs')}`).join(' · '))}</span></span>${layout.columns.includes('duration')?`<span>${fmtDur(song.duration)}</span>`:''}${songActionButtons(song)}<button class="btn btn-ghost" data-song-menu aria-label="Actions for ${esc(mediaTitle(song))}">⋮</button></div>`).join('');
 const albumRows=data.albums.map(item=>`<div class="media-row" role="button" tabindex="0" data-album="${esc(item.album_key)}" aria-label="Open album ${esc(item.title)} by ${esc(item.artist)}"><span>${esc(item.title)}<span class="text-muted text-sm"> · ${esc(item.artist)}${item.year ? ` · ${item.year}`:''}</span></span></div>`).join('');
 const artistRows=data.artists.map(item=>`<div class="media-row" role="button" tabindex="0" data-artist-card="${esc(item.artist_key)}" aria-label="Open artist ${esc(item.name)}"><span>${esc(item.name)}<span class="text-muted text-sm"> · ${item.album_count} albums · ${item.song_count} songs</span></span></div>`).join('');
 const fileRows=files.map((item,index)=>`<div class="media-row" role="group" tabindex="0" data-id="${item.id}" data-index="${songs.length+index}" data-search-kind="file" aria-label="Open file ${esc(item.file_name)}"><span class="music-song"><button class="btn btn-ghost song-open" data-action="search-file-open" aria-label="Open file ${esc(item.file_name)}">${esc(item.file_name)}</button><span class="text-muted text-sm">${esc(item.rel_path)}</span></span></div>`).join('');
 const total=songs.length+data.albums.length+data.artists.length+files.length;
 grid.innerHTML=header+(total ? '' : '<p class="grid-full" role="status">Nothing matches these filters. <button class="btn" data-clear-search>Clear filters</button></p>')+musicLayoutToolbar('songs',layout)+group('Songs',songs,songRows)+group('Albums',data.albums,albumRows)+group('Artists',data.artists,artistRows)+group('Files',files,fileRows);
 grid.classList.add('music-detail');
 document.getElementById('result-count').textContent=`Search · ${total} results`;
 wire();bindSongActions(grid);
 grid.querySelector('[data-clear-search]')?.addEventListener('click',()=>{state.music={search:true,query:''}; const input=document.getElementById('search-input');if(input){input.value='';input.focus();}reload();});
 grid.querySelectorAll('[data-album]').forEach(row=>row.onclick=()=>openAlbum(row.dataset.album));
 grid.querySelectorAll('[data-artist-card]').forEach(row=>row.onclick=()=>openArtist(row.dataset.artistCard));
 bindSearchPlayback(grid);
 grid.querySelectorAll('[data-song-menu]').forEach(button=>button.onclick=()=>openSongActions(button.closest('.media-row')));
 const saved=view.selected;
 const target=[...grid.querySelectorAll('.media-row')].find(row=>saved && Object.entries(saved).every(([key,value])=>row.dataset[key]===value));
 if(target && document.activeElement?.id!=='search-input'){selectByElement(target);focusSelectedCard();globalThis.scrollTo?.(0,view.scroll || 0);}
 bindMusicLayout(grid,'songs',layout,reload,()=>{const selected=currentElement();state.music.selected=selected?.dataset.id?{id:selected.dataset.id}:selected?.dataset.album?{album:selected.dataset.album}:selected?.dataset.artistCard?{artistCard:selected.dataset.artistCard}:null;state.music.scroll=globalThis.scrollY;});
}

async function searchFiles(query,current) {
 const result=[];
 for(let offset=0;current();offset+=1000) {
  const response=await API.media({search:query,limit:1000,offset,sort:'name'});
  const page=Array.isArray(response)?response:response?.media || [];
  result.push(...page);
  if(page.length<1000) break;
 }
 return result;
}

// Explicit file buttons work directly on touch and supply the existing row's
// Media actions menu with the same action; Sets keep their existing touch policy.
export function bindSearchPlayback(grid) {
 const play=row=>{selectByElement(row);document.dispatchEvent(new Event('search:play-selected'));};
 grid.querySelectorAll('[data-search-kind]').forEach(row=>{
  row.onclick=event=>{if(event.target.closest('[data-action], [data-song-menu]'))return;play(row);};
  const open=row.querySelector('[data-action="search-file-open"]');
  if(open) open.onclick=event=>{event.stopPropagation();play(row);};
 });
}
