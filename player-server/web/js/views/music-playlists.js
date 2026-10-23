import { collectionButtons, bindCollectionActions, bindCollectionCatalog } from '../collection-actions.js';
import { API } from '../api.js';
import { state } from '../state.js';
import { escapeHtml as esc, fmtDur, toast } from '../utils.js';
import { selectByElement, focusSelectedCard } from '../selection.js';
import { playAlbumCollection } from '../queue.js';
import { mediaTitle } from '../music.js';
import { openSongActions } from '../queue-actions.js';

let reload = () => {};
let open = () => {};
let back = () => {};
let generation = 0;
export function initPlaylists(load, navigate, goBack) { reload=load;open=navigate;back=goBack; }
const cover = p => p.cover_media_id ? `<img class="album-cover" src="/api/media/${p.cover_media_id}/thumbnail" alt="Artwork for ${esc(p.name)}">` : '<div class="album-cover album-placeholder" role="img" aria-label="No playlist artwork">♫</div>';
const accessLabel = p => `${p.visibility === 'shared' ? 'Shared with server' : 'Private'} · Owner: ${p.owner_name || ''}${p.can_edit === false ? ' · Read only' : ''}`;
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
  grid.innerHTML=`<div class="grid-full music-heading"><button class="btn btn-ghost" data-back>← Back to Sets</button><h1>Playlists</h1><button class="btn btn-primary" data-create>Create playlist</button></div>`+(p.length?p.map(item=>`<div class="media-card album-card" role="button" tabindex="0" data-playlist="${esc(item.playlist_id)}" aria-label="Open playlist ${esc(item.name)}">${cover(item)}<div class="card-body"><div class="title" title="${esc(item.name)}">${esc(item.name)}</div><p>${totals(item)}</p><p>${esc(accessLabel(item))}</p></div></div>`).join(''):'<p class="grid-full">No playlists yet. Create a playlist to save music for later.</p>');
  grid.querySelector('[data-create]').onclick=()=>editPlaylist(null,active);
  grid.querySelectorAll('[data-playlist]').forEach(card=>card.onclick=()=>open(card.dataset.playlist));
  bindCollectionCatalog(grid,p,'playlist','playlist_id','name',active);
 }else{
  renderPlaylistDetail(grid,p,active);
 }
 grid.querySelector('[data-back]').onclick=back;
 const target=(saved?.playlist ? grid.querySelector(`[data-playlist="${CSS.escape(saved.playlist)}"]`) : null)||grid.querySelector('[data-playlist],.media-row');if(target){selectByElement(target);focusSelectedCard();if(saved)window.scrollTo(0,saved.scroll);}else grid.querySelector(detail?'[data-add]:not([hidden]),[data-find]':'[data-create]')?.focus();
}
function dialog(title,content){
 const modal=document.getElementById('playlist-modal');
 modal.innerHTML=`<div class="modal" role="dialog" aria-modal="true" aria-labelledby="playlist-dialog-title"><div class="modal-header"><h2 id="playlist-dialog-title">${esc(title)}</h2><button class="btn btn-ghost" data-close aria-label="Close playlist dialog">✕</button></div><div class="modal-body">${content}<p role="alert" data-error></p></div></div>`;
 const close=()=>modal.classList.remove('open');modal.querySelector('[data-close]').onclick=close;
 modal.querySelectorAll('[data-cancel]').forEach(b=>b.onclick=close);
 modal.classList.add('open');const panel=modal.firstElementChild;const alive=()=>modal.classList.contains('open')&&modal.firstElementChild===panel;return {modal,close,alive};
}
async function editPlaylist(p,current){
 let users=[];
 if(p?.can_transfer){try{users=await API.users();if(!current())return;}catch(e){toast(e.message||'Could not load owners. Check your connection.','error');return;}}
 const {modal,close,alive}=dialog(p?'Edit playlist':'Create playlist',`<form><label>Name<input name="name" aria-label="Name" required value="${esc(p?.name||'')}"></label><label>Comment<textarea name="comment" aria-label="Comment" rows="4">${esc(p?.comment||'')}</textarea></label><label>Visibility<select name="visibility" aria-label="Visibility"><option value="private" ${p?.visibility!=='shared'?'selected':''}>Private</option><option value="shared" ${p?.visibility==='shared'?'selected':''}>Shared with server</option></select></label><p>Shared playlists can be read by signed-in accounts on this server. This does not create a public link or grant access to music.</p>${p?.can_transfer?`<label>Owner<select name="owner" aria-label="Owner">${users.map(u=>`<option value="${u.id}" ${u.id===p.owner_id?'selected':''}>${esc(u.username)}</option>`).join('')}</select></label>`:''}<p class="text-muted">Name: 1–200 characters. Comment: up to 2000 characters.</p><div class="playlist-actions"><button type="button" class="btn" data-cancel>Cancel</button><button class="btn btn-primary" type="submit">${p?'Save changes':'Create playlist'}</button></div></form>`);
 modal.querySelector('form').onsubmit=async e=>{
  e.preventDefault();const name=modal.querySelector('[name=name]').value.trim(),comment=modal.querySelector('[name=comment]').value;
  if([...name].length<1||[...name].length>200||[...comment].length>2000){modal.querySelector('[data-error]').textContent='Name must be 1–200 characters and comment at most 2000 characters.';return;}
  const visibility=modal.querySelector('[name=visibility]').value;
  const owner=p?.can_transfer?Number(modal.querySelector('[name=owner]').value):null;
  if(owner!==null&&owner!==p.owner_id&&!window.confirm('Transfer ownership? The previous owner will lose edit access unless they are an administrator.'))return;
  const body={name,comment,visibility,...(owner!==null&&owner!==p.owner_id?{owner_id:owner}:{})};
  const submit=modal.querySelector('[type=submit]');submit.disabled=true;
  try{const saved=p?await API.updatePlaylist(p.playlist_id,body):await API.createPlaylist(body);if(!current()||!alive())return;close();toast('Playlist saved.','success');p?reload():open(saved.playlist_id);}catch(e){if(!alive()||!current())return;modal.querySelector('[data-error]').textContent=e.message||'Could not save playlist. Check your connection.';}finally{if(alive())submit.disabled=false;}
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

function matchesSong(song,query){return [mediaTitle(song),song.file_name,song.music?.artist,song.music?.album].join(' ').toLocaleLowerCase().includes(query.trim().toLocaleLowerCase());}
function renderPlaylistDetail(grid,initial,current){
 let p=initial, busy=false;
 let undo=null;
 grid.innerHTML=`<div class="grid-full music-heading"><button class="btn btn-ghost" data-back>← Back to playlists</button><div class="album-header"><div data-playlist-cover>${cover(p)}</div><div><h1>${esc(p.name)}</h1><p>${esc(accessLabel(p))}</p><p class="playlist-comment">${esc(p.comment)}</p><p data-totals>${totals(p)}</p><div class="playlist-actions">${collectionButtons('Play playlist')}<button class="btn btn-ghost" data-playlist-actions aria-label="Playlist actions">Playlist actions ⋮</button><button class="btn" data-add>Add music</button><button class="btn" data-add-songs>Add songs</button><button class="btn" data-edit>Edit playlist</button><button class="btn btn-ghost" data-delete>Delete playlist</button></div></div></div><label>Find songs<input type="search" data-find aria-label="Find playlist songs"></label><button class="btn btn-ghost" data-clear>Clear</button><p role="status" data-count></p><p role="alert" data-song-error></p><div class="playlist-actions" data-undo hidden><span>Song removed from playlist.</span><button class="btn" data-restore>Undo</button></div></div><div class="grid-full" data-rows></div>`;
 const rows=grid.querySelector('[data-rows]'),find=grid.querySelector('[data-find]');
 const draw=(focusID=null)=>{
  state.media=p.songs;
  const visible=p.songs.map((song,index)=>({song,index,entry:p.entries[index]})).filter(({song})=>matchesSong(song,find.value));
  grid.querySelector('[data-totals]').textContent=totals(p);
  grid.querySelector('[data-playlist-cover]').innerHTML=cover(p);
  grid.querySelectorAll('[data-collection-play], [data-collection-menu]').forEach(button=>button.disabled=!p.songs.length);
  grid.querySelector('[data-count]').textContent=`Showing ${visible.length} of ${p.songs.length} songs`;
  rows.innerHTML=visible.length?visible.map(({song,index,entry})=>`<div class="media-row" role="group" tabindex="0" data-id="${song.id}" data-index="${index}" data-entry="${esc(entry.entry_id)}" aria-label="Play ${esc(mediaTitle(song))}"><span class="text-muted music-number">${index+1}</span><span class="music-song"><button class="btn btn-ghost song-open" title="${esc(mediaTitle(song))}">${esc(mediaTitle(song))}</button><span class="text-muted text-sm">${esc(song.music?.artist||'Unknown artist')} · ${esc(song.music?.album||'')}</span></span><span>${fmtDur(song.duration)}</span><span hidden>${(p.can_edit===false?[]:[['up','Move up'],['down','Move down'],['remove','Remove from playlist']]).map(([action,label])=>`<button data-action="playlist-${action}" ${busy||(action==='up'&&index===0)||(action==='down'&&index===p.songs.length-1)?'disabled':''}>${label}</button>`).join('')}</span><button class="btn btn-ghost" data-song-menu aria-label="Song actions for ${esc(mediaTitle(song))}">⋮</button></div>`).join(''):`<p>${p.songs.length?'No matching songs. Clear the search to see all songs.':'This playlist is empty.'}</p>`;
  rows.querySelectorAll('.media-row').forEach(row=>{
   row.onclick=e=>{if(e.target.closest('[data-song-menu],[data-action]'))return;selectByElement(row);playAlbumCollection(p.songs,Number(row.dataset.index),p.name);};
   row.querySelector('[data-song-menu]').onclick=()=>openSongActions(row);
   row.querySelectorAll('[data-action]').forEach(button=>button.onclick=()=>mutate(button.dataset.action.slice(9),row));
  });
  const target=(focusID&&rows.querySelector(`[data-entry="${CSS.escape(focusID)}"]`))||rows.querySelector('.media-row');
  if(target){selectByElement(target);if(focusID)focusSelectedCard();}else if(focusID)find.focus();
  grid.querySelector('[data-undo]').hidden=!undo;
 };
 const save=async(body,focusID)=>{
  if(busy||p.can_edit===false)return false;busy=true;grid.querySelector('[data-song-error]').textContent='';
  try{const result=await API.editPlaylistSongs(p.playlist_id,body);if(!current())return false;const added=focusID==='restored'?result.entries.find(entry=>!p.entries.some(old=>old.entry_id===entry.entry_id))?.entry_id:null;p=result;busy=false;draw(added||focusID);return true;}
  catch(e){if(current()){grid.querySelector('[data-song-error]').textContent=e.message||'Could not update playlist. Check your connection.';focusSelectedCard();}return false;}
  finally{busy=false;}
 };
 const mutate=async(action,row)=>{
  const index=Number(row.dataset.index),entryID=row.dataset.entry;
  const removed={media:p.songs[index].id,before:p.entries[index+1]?.entry_id||''};
  const next=action==='remove'?(row.nextElementSibling?.dataset.entry||row.previousElementSibling?.dataset.entry||'search'):entryID;
  if(await save({action,entry_id:entryID},next)){
   if(action==='remove'){undo=removed;grid.querySelector('[data-undo]').hidden=false;}
   toast(action==='remove'?'Song removed from playlist.':'Song moved.','success');
  }
 };
 find.oninput=()=>draw();grid.querySelector('[data-clear]').onclick=()=>{find.value='';draw();find.focus();};
 bindCollectionActions(grid.querySelector('[data-collection-actions]'),()=>[{kind:'playlist',key:p.playlist_id}],current,p.name);
 grid.querySelectorAll('[data-add],[data-add-songs],[data-edit],[data-delete]').forEach(button=>button.hidden=p.can_edit===false);
 grid.querySelector('[data-playlist-actions]').onclick=()=>playlistActions(p,current);
 grid.querySelector('[data-add]').onclick=()=>addMusic(p,current);
 grid.querySelector('[data-add-songs]').onclick=()=>addSongs(p,current);
 grid.querySelector('[data-edit]').onclick=()=>editPlaylist(p,current);
 grid.querySelector('[data-delete]').onclick=()=>deletePlaylist(p,current);
 grid.querySelector('[data-restore]').onclick=async()=>{const saved=undo;if(!saved)return;if(await save({action:'add',media_ids:[saved.media],before_entry_id:saved.before},'restored')){undo=null;grid.querySelector('[data-undo]').hidden=true;toast('Song restored to playlist.','success');}};
 draw();
}
async function addSongs(p,current){
 const {modal,close,alive}=dialog('Add songs','<p role="status">Loading songs…</p>');
 const body=modal.querySelector('.modal-body');
 const selected=new Set();
 let saving=false;
 const load=async()=>{
  try{
   const albums=await API.albums();
   const details=await Promise.all(albums.map(a=>API.album(a.album_key)));
   if(!current()||!alive())return;
   const songs=[...new Map(details.flatMap(a=>a.songs).map(song=>[song.id,song])).values()];
   body.innerHTML='<label>Find songs<input type="search" aria-label="Find songs to add" autofocus></label><p>Repeated songs are added again.</p><p role="status" data-selected>0 selected</p><div class="playlist-albums" data-choices></div><p role="alert" data-error></p><div class="playlist-actions"><button class="btn" data-cancel>Cancel</button><button class="btn btn-primary" data-save disabled>Add selected songs</button></div>';
   const input=body.querySelector('input'),choices=body.querySelector('[data-choices]'),submit=body.querySelector('[data-save]');
   const draw=()=>{const matches=songs.filter(song=>matchesSong(song,input.value));choices.innerHTML=matches.length?matches.map(song=>`<label class="playlist-song-choice"><input type="checkbox" value="${song.id}" ${selected.has(song.id)?'checked':''} ${saving?'disabled':''} aria-label="Select ${esc(mediaTitle(song))}"><span>${esc(mediaTitle(song))}<span class="text-muted text-sm"> · ${esc(song.music?.artist||'Unknown artist')} · ${esc(song.music?.album||'')}</span></span></label>`).join(''):`<p>${songs.length?'No matching songs.':'No songs available. Browse Sets or ask an administrator to rescan music tags.'}</p>`;choices.querySelectorAll('input').forEach(check=>check.onchange=()=>{if(saving)return;check.checked?selected.add(Number(check.value)):selected.delete(Number(check.value));body.querySelector('[data-selected]').textContent=`${selected.size} selected`;submit.disabled=saving||!selected.size;});};
   input.oninput=draw;body.querySelector('[data-cancel]').onclick=close;
   submit.onclick=async()=>{if(saving||!selected.size)return;saving=true;submit.disabled=true;choices.querySelectorAll('input').forEach(check=>check.disabled=true);body.querySelector('[data-error]').textContent='';try{await API.editPlaylistSongs(p.playlist_id,{action:'add',media_ids:songs.filter(song=>selected.has(song.id)).map(song=>song.id)});if(!current()||!alive())return;close();toast('Songs added to playlist.','success');reload();}catch(e){if(current()&&alive())body.querySelector('[data-error]').textContent=e.message||'Could not add songs. Check your connection.';}finally{if(alive()){saving=false;choices.querySelectorAll('input').forEach(check=>check.disabled=false);submit.disabled=!selected.size;}}};
   draw();input.focus();
  }catch(e){if(!current()||!alive())return;body.innerHTML='<p role="alert">Could not load songs. Check your connection.</p><button class="btn" data-retry>Retry</button>';body.querySelector('[data-retry]').onclick=load;}
 };
 await load();
}

function playlistActions(p,current){
 const {modal,close}=dialog('Playlist actions','<button class="btn" data-export>Export M3U8…</button>');
 modal.querySelector('[data-export]').onclick=()=>{close();exportPlaylist(p,current);};
}
function exportPlaylist(p,current){
 const filename=`playlist-${p.playlist_id}.m3u8`;
 const {modal,close,alive}=dialog('Export playlist',`<p class="playlist-comment">${esc(p.name)}</p><p>UTF-8 M3U8 · <span class="playlist-comment">${esc(filename)}</span></p><p>Paths are relative to the server’s media root. Save this file beside the corresponding library folders in the other tool.</p><p>Only songs you can access with exportable paths are included; repeated songs keep their order. This file does not grant access to music.</p><p role="status" data-export-status></p><div class="playlist-actions"><button class="btn" data-cancel>Cancel</button><button class="btn btn-primary" data-download>Download M3U8</button></div>`);
 const button=modal.querySelector('[data-download]');
 button.onclick=async()=>{
  button.disabled=true;modal.querySelector('[data-error]').textContent='';modal.querySelector('[data-export-status]').textContent='Preparing…';
  try{
   const data=await API.exportPlaylist(p.playlist_id);
   if(!alive()||!current())return;
   const url=URL.createObjectURL(new Blob([data],{type:'application/vnd.apple.mpegurl;charset=utf-8'}));
   const link=document.createElement('a');link.href=url;link.download=filename;document.body.append(link);link.click();link.remove();
   window.setTimeout(()=>URL.revokeObjectURL(url),60000);
   close();toast('Playlist download started.','success');
  }catch(e){if(alive()&&current())modal.querySelector('[data-error]').textContent=e.message||'Could not export playlist. Check your connection and try again.';}
  finally{if(alive()){button.disabled=false;modal.querySelector('[data-export-status]').textContent='';}}
 };
}
