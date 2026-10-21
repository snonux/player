import { API } from '../api.js';
import { state } from '../state.js';
import { escapeHtml as esc, fmtDur, toast } from '../utils.js';
import { selectByElement, focusSelectedCard } from '../selection.js';
import { playAlbumCollection } from '../queue.js';
import { mediaTitle } from '../music.js';

let reload = () => {};
let open = () => {};
let back = () => {};
let generation = 0;
export function initPlaylists(load, navigate, goBack) { reload=load;open=navigate;back=goBack; }
const cover = p => p.cover_media_id ? `<img class="album-cover" src="/api/media/${p.cover_media_id}/thumbnail" alt="Artwork for ${esc(p.name)}">` : '<div class="album-cover album-placeholder" role="img" aria-label="No playlist artwork">♫</div>';
const totals = p => `${p.song_count} songs · ${fmtDur(p.duration)}`;
export async function renderPlaylists(grid, view, current, saved) {
 const own=++generation;
 const active=()=>own===generation&&current();
 const detail=Boolean(view.playlist);
 const p=detail?await API.playlist(view.playlist):await API.playlists();
 if (!active()) return;
 const count=document.getElementById('result-count');count.textContent=detail?`Music · Playlists · ${p.name}`:'Music · Playlists';
 grid.classList.toggle('music-detail',detail);
 if(!detail){
  grid.innerHTML=`<div class="grid-full music-heading"><button class="btn btn-ghost" data-back>← Back to Sets</button><h1>Playlists</h1><button class="btn btn-primary" data-create>Create playlist</button></div>`+(p.length?p.map(item=>`<div class="media-card album-card" role="button" tabindex="0" data-playlist="${esc(item.playlist_id)}" aria-label="Open playlist ${esc(item.name)}">${cover(item)}<div class="card-body"><div class="title" title="${esc(item.name)}">${esc(item.name)}</div><p>${totals(item)}</p></div></div>`).join(''):'<p class="grid-full">No playlists yet. Create a playlist to save music for later.</p>');
  grid.querySelector('[data-create]').onclick=()=>editPlaylist(null,active);
  grid.querySelectorAll('[data-playlist]').forEach(card=>card.onclick=()=>open(card.dataset.playlist));
 }else{
  state.media=p.songs;
  grid.innerHTML=`<div class="grid-full music-heading"><button class="btn btn-ghost" data-back>← Back to playlists</button><div class="album-header">${cover(p)}<div><h1>${esc(p.name)}</h1><p class="playlist-comment">${esc(p.comment)}</p><p>${totals(p)}</p><div class="playlist-actions"><button class="btn btn-primary" data-play ${p.songs.length?'':'disabled'}>▶ Play playlist</button><button class="btn ${p.songs.length?'':'btn-primary'}" data-add>Add music</button><button class="btn" data-edit>Edit playlist</button><button class="btn btn-ghost" data-delete>Delete playlist</button></div></div></div>${p.songs.length?'':'<p>This playlist is empty.</p>'}</div>`+p.songs.map((song,index)=>`<div class="media-row" role="group" tabindex="0" data-id="${song.id}" data-index="${index}" aria-label="Play ${esc(mediaTitle(song))}"><span class="music-song"><button class="btn btn-ghost song-open">${esc(mediaTitle(song))}</button><span class="text-muted text-sm">${esc(song.music?.artist||'Unknown artist')}</span></span><span>${fmtDur(song.duration)}</span></div>`).join('');
  grid.querySelector('[data-play]').onclick=()=>playAlbumCollection(p.songs,0,p.name);
  grid.querySelectorAll('.media-row').forEach(row=>row.onclick=()=>{selectByElement(row);playAlbumCollection(p.songs,Number(row.dataset.index),p.name);});
  grid.querySelector('[data-edit]').onclick=()=>editPlaylist(p,active);
  grid.querySelector('[data-add]').onclick=()=>addMusic(p,active);
  grid.querySelector('[data-delete]').onclick=()=>deletePlaylist(p,active);
 }
 grid.querySelector('[data-back]').onclick=back;
 const target=(saved?.playlist ? grid.querySelector(`[data-playlist="${CSS.escape(saved.playlist)}"]`) : null)||grid.querySelector('[data-playlist],.media-row');if(target){selectByElement(target);focusSelectedCard();if(saved)window.scrollTo(0,saved.scroll);}else grid.querySelector(detail?'[data-add]':'[data-create]')?.focus();
}
function dialog(title,content){
 const modal=document.getElementById('playlist-modal');
 modal.innerHTML=`<div class="modal" role="dialog" aria-modal="true" aria-labelledby="playlist-dialog-title"><div class="modal-header"><h2 id="playlist-dialog-title">${esc(title)}</h2><button class="btn btn-ghost" data-close aria-label="Close playlist dialog">✕</button></div><div class="modal-body">${content}<p role="alert" data-error></p></div></div>`;
 const close=()=>modal.classList.remove('open');modal.querySelector('[data-close]').onclick=close;
 modal.querySelectorAll('[data-cancel]').forEach(b=>b.onclick=close);
 modal.classList.add('open');const panel=modal.firstElementChild;const alive=()=>modal.classList.contains('open')&&modal.firstElementChild===panel;return {modal,close,alive};
}
function editPlaylist(p,current){
 const {modal,close,alive}=dialog(p?'Edit playlist':'Create playlist',`<form><label>Name<input name="name" aria-label="Name" required value="${esc(p?.name||'')}"></label><label>Comment<textarea name="comment" aria-label="Comment" rows="4">${esc(p?.comment||'')}</textarea></label><p class="text-muted">Name: 1–200 characters. Comment: up to 2000 characters.</p><div class="playlist-actions"><button type="button" class="btn" data-cancel>Cancel</button><button class="btn btn-primary" type="submit">${p?'Save changes':'Create playlist'}</button></div></form>`);
 modal.querySelector('form').onsubmit=async e=>{
  e.preventDefault();const name=modal.querySelector('[name=name]').value.trim(),comment=modal.querySelector('[name=comment]').value;
  if([...name].length<1||[...name].length>200||[...comment].length>2000){modal.querySelector('[data-error]').textContent='Name must be 1–200 characters and comment at most 2000 characters.';return;}
  const submit=modal.querySelector('[type=submit]');submit.disabled=true;
  try{const saved=p?await API.updatePlaylist(p.playlist_id,{name,comment}):await API.createPlaylist({name,comment});if(!current()||!alive())return;close();toast('Playlist saved.','success');p?reload():open(saved.playlist_id);}catch(e){if(!alive()||!current())return;modal.querySelector('[data-error]').textContent=e.message||'Could not save playlist. Check your connection.';}finally{if(alive())submit.disabled=false;}
 };
}
async function addMusic(p,current){
 const {modal,close,alive}=dialog('Add music','<p role="status">Loading albums…</p>');
 const body=modal.querySelector('.modal-body');
 const load=async()=>{try{const albums=await API.albums();if(!current()||!alive())return;body.innerHTML=`<p>Choose an album to add its songs in album order.</p><div class="playlist-albums">${albums.length?albums.map(a=>`<button class="btn" data-add-album="${esc(a.album_key)}">${esc(a.title)} — ${esc(a.artist)}</button>`).join(''):'<p>No albums available. Browse Sets or ask an administrator to rescan music tags.</p>'}</div><p role="alert" data-error></p><button class="btn" data-cancel>Cancel</button>`;body.querySelector('[data-cancel]').onclick=close;body.querySelectorAll('[data-add-album]').forEach(button=>button.onclick=async()=>{body.querySelectorAll('[data-add-album]').forEach(b=>b.disabled=true);try{await API.addPlaylistAlbum(p.playlist_id,button.dataset.addAlbum);if(!current()||!alive())return;close();toast('Album added to playlist.','success');reload();}catch(e){if(!alive()||!current())return;body.querySelector('[data-error]').textContent=e.message||'Could not add music. Check your connection.';}finally{if(alive())body.querySelectorAll('[data-add-album]').forEach(b=>b.disabled=false);}});}catch(e){if(!current()||!alive())return;body.innerHTML='<p role="alert">Could not load albums. Check your connection.</p><button class="btn" data-retry>Retry</button>';body.querySelector('[data-retry]').onclick=load;}};
 await load();
}
function deletePlaylist(p,current){
 const {modal,close,alive}=dialog('Delete playlist',`<p>Delete “${esc(p.name)}”? Music files stay available in your library.</p><div class="playlist-actions"><button class="btn" data-cancel>Cancel</button><button class="btn btn-primary" data-confirm>Delete playlist</button></div>`);
 const button=modal.querySelector('[data-confirm]');button.onclick=async()=>{button.disabled=true;try{await API.deletePlaylist(p.playlist_id);if(!current()||!alive())return;close();toast('Playlist deleted.','success');back();}catch(e){if(!alive()||!current())return;modal.querySelector('[data-error]').textContent=e.message||'Could not delete playlist.';}finally{if(alive())button.disabled=false;}};
}
