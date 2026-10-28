import { openPlaylistRules, playlistRulesSummary } from './music-playlist-rules.js';
import { t, html, plural, ui, uiCount } from '../i18n.js';
import { mountMusicArtwork } from '../music-artwork.js';
import { favoriteButton, bindFavorites, favoriteFilter } from '../music-favorites.js';
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
let pendingRulesID = "";
export function initPlaylists(load, navigate, goBack) { reload=load;open=navigate;back=goBack; document.addEventListener('language:changed',()=>{document.querySelectorAll('[data-rule-summary]').forEach(node=>{try{node.textContent=playlistRulesSummary(JSON.parse(node.dataset.ruleSummary));}catch{}});}); }
const cover = p => p.artwork_revision ? html`<img class="album-cover" src="/api/music/artwork/playlist/${encodeURIComponent(p.playlist_id)}/image?revision=${encodeURIComponent(p.artwork_revision)}" alt="Artwork for ${esc(p.name)}" data-custom-playlist-art data-cover-fallback="${p.cover_media_id || ''}">` : p.cover_media_id ? html`<img class="album-cover" src="/api/media/${p.cover_media_id}/thumbnail" alt="Artwork for ${esc(p.name)}">` : html`<div class="album-cover album-placeholder" role="img" aria-label="No playlist artwork">♫</div>`;
const accessLabel = p => `${ui(p.visibility === 'shared' ? 'Shared with server' : 'Private')} · ${ui('Owner')}: ${esc(p.owner_name || '')}${p.can_edit === false ? ' · '+ui('Read only') : ''}${p.source ? ' · '+ui('Following source') : ''}`;
const totals = p => `${uiCount(p.song_count, '{count} song', '{count} songs')} · ${fmtDur(p.duration)}`;
export async function renderPlaylists(grid, view, current, saved) {
 const own=++generation;
 const active=()=>own===generation&&current();
 const detail=Boolean(view.playlist);
 const p=detail?await API.playlist(view.playlist):await API.playlists({favorite:view.favorites || false});
 if (!active()) return;
 const count=document.getElementById('result-count');count.innerHTML=detail?`${ui('Music · Playlists')} · ${esc(p.name)}`:ui('Music · Playlists');
 grid.classList.toggle('music-detail',detail);
 grid.classList.toggle('music-table',!detail && playlistListDensity()==='compact');
 grid.classList.toggle('playlist-list',!detail);
 if(!detail){
  const compact=playlistListDensity()==='compact';
  grid.innerHTML=html`<div class="grid-full music-heading"><button class="btn btn-ghost" data-back>← Back to Sets</button><h1>Playlists</h1><button type="button" class="icon-btn" data-new-menu aria-label="New" aria-haspopup="menu" title="New">☰</button><button class="btn btn-ghost" data-density aria-pressed="${compact?'true':'false'}">${ui(compact?'Grid view':'Compact list')}</button></div>`+(p.length?p.map(item=>html`<div class="media-card album-card" role="button" tabindex="0" data-playlist="${esc(item.playlist_id)}" aria-label="Open playlist ${esc(item.name)}">${cover(item)}<div class="card-body">${favoriteButton('playlist',item.playlist_id,item)}<div class="title" title="${esc(item.name)}">${esc(item.name)}</div><p>${totals(item)}</p><p class="playlist-list-meta">${accessLabel(item)}</p></div></div>`).join(''):html`<p class="grid-full">${view.favorites?'No favorite playlists. Show all playlists to favorite one.':'No playlists yet. Create a playlist to save music for later.'}</p>`);
  grid.querySelector('[data-new-menu]').onclick=event=>playlistNewMenu(event.currentTarget,active);
  grid.querySelector('[data-density]').onclick=()=>{savePlaylistListDensity(compact?'grid':'compact');reload();};
  grid.querySelectorAll('[data-playlist]').forEach(card=>card.onclick=()=>open(card.dataset.playlist));
  bindCollectionCatalog(grid,p,'playlist','playlist_id','name',active);
 }else{
  renderPlaylistDetail(grid,p,active);
 }
 bindArtworkFallbacks(grid);
 bindFavorites(grid,active);
 if(!detail)favoriteFilter(grid,view,reload);
 grid.querySelector('[data-back]').onclick=back;
 const target=(saved?.playlist ? grid.querySelector(`[data-playlist="${CSS.escape(saved.playlist)}"]`) : null)||grid.querySelector('[data-playlist],.media-row');if(target){selectByElement(target);focusSelectedCard();if(saved)window.scrollTo(0,saved.scroll);}else grid.querySelector(detail?'[data-add-songs]:not([hidden]),[data-find]':'[data-new-menu]')?.focus();
}
const playlistDensityKey='player.playlist-list-density.v1';
function playlistListDensity(){try{return localStorage.getItem(playlistDensityKey)==='grid'?'grid':'compact';}catch{return 'compact';}}
function savePlaylistListDensity(value){try{localStorage.setItem(playlistDensityKey,value);}catch{/* Preference is optional. */}}
function playlistNewMenu(button,current){
 const popover=document.createElement('div');
 popover.className='shortcut-picker';popover.setAttribute('popover','auto');popover.setAttribute('role','menu');popover.setAttribute('aria-label',t('New playlist'));
 popover.innerHTML=html`<button class="btn btn-ghost" data-create>Create playlist</button><button class="btn btn-ghost" data-smart>Create smart playlist</button><button class="btn btn-ghost" data-import>Import M3U…</button>`;
 popover.querySelector('[data-create]').onclick=()=>{popover.hidePopover();editPlaylist(null,current);};
 popover.querySelector('[data-smart]').onclick=()=>{popover.hidePopover();editPlaylist(null,current,true);};
 popover.querySelector('[data-import]').onclick=()=>{popover.hidePopover();importPlaylist(current);};
 popover.addEventListener('toggle',event=>{if(event.newState==='closed'){popover.remove();if(button.isConnected)button.focus();}});
 document.body.append(popover);popover.showPopover();popover.querySelector('button').focus();
}
function dialog(title,content){
 const modal=document.getElementById('playlist-modal');
 modal.innerHTML=playlistDialogContent(title,content);
 const close=()=>modal.classList.remove('open');modal.querySelector('[data-close]').onclick=close;
 modal.querySelectorAll('[data-cancel]').forEach(b=>b.onclick=close);
 modal.classList.add('open');const panel=modal.firstElementChild;const alive=()=>modal.classList.contains('open')&&modal.firstElementChild===panel;return {modal,close,alive};
}
async function editPlaylist(p,current,smart=false){
 let users=[];
 if(p?.can_transfer){try{users=await API.users();if(!current())return;}catch(e){toast(e.message||'Could not load owners. Check your connection.','error');return;}}
 const {modal,close,alive}=dialog(p?'Edit playlist':smart?'Create smart playlist':'Create playlist',playlistEditorContent(p,users));
 modal.querySelector('form').onsubmit=async e=>{
  e.preventDefault();const name=modal.querySelector('[name=name]').value.trim(),comment=modal.querySelector('[name=comment]').value;
  if([...name].length<1||[...name].length>200||[...comment].length>2000){modal.querySelector('[data-error]').textContent='Name must be 1–200 characters and comment at most 2000 characters.';return;}
  const visibility=modal.querySelector('[name=visibility]').value;
  const owner=p?.can_transfer?Number(modal.querySelector('[name=owner]').value):null;
  if(owner!==null&&owner!==p.owner_id&&!window.confirm('Transfer ownership? The previous owner will lose edit access unless they are an administrator.'))return;
  const body={name,comment,visibility,...(owner!==null&&owner!==p.owner_id?{owner_id:owner}:{})};
  const submit=modal.querySelector('[type=submit]');submit.disabled=true;
  try{const saved=p?await API.updatePlaylist(p.playlist_id,body):await API.createPlaylist(body);if(!current()||!alive())return;close();toast(t('Playlist saved.'),'success');if(smart)pendingRulesID=saved.playlist_id;p?reload():open(saved.playlist_id);}catch(e){if(!alive()||!current())return;modal.querySelector('[data-error]').textContent=e.message||'Could not save playlist. Check your connection.';}finally{if(alive())submit.disabled=false;}
 };
}
async function addMusic(p,current){
 const {modal,close,alive}=dialog('Add music',html`<p role="status">Loading albums…</p>`);
 const body=modal.querySelector('.modal-body');
 const load=async()=>{try{const albums=await API.albums();if(!current()||!alive())return;body.innerHTML=html`<p>Choose an album to add its songs in album order.</p><div class="playlist-albums">${albums.length?albums.map(a=>html`<button class="btn" data-add-album="${esc(a.album_key)}">${esc(a.title)} — ${esc(a.artist)}</button>`).join(''):html`<p>No albums available. Browse Sets or ask an administrator to rescan music tags.</p>`}</div><p role="alert" data-error></p><button class="btn" data-cancel>Cancel</button>`;body.querySelector('[data-cancel]').onclick=close;body.querySelectorAll('[data-add-album]').forEach(button=>button.onclick=async()=>{body.querySelectorAll('[data-add-album]').forEach(b=>b.disabled=true);try{await API.addPlaylistAlbum(p.playlist_id,button.dataset.addAlbum);if(!current()||!alive())return;close();toast('Album added to playlist.','success');reload();}catch(e){if(!alive()||!current())return;body.querySelector('[data-error]').textContent=e.message||'Could not add music. Check your connection.';}finally{if(alive())body.querySelectorAll('[data-add-album]').forEach(b=>b.disabled=false);}});}catch(e){if(!current()||!alive())return;body.innerHTML=html`<p role="alert">Could not load albums. Check your connection.</p><button class="btn" data-retry>Retry</button>`;body.querySelector('[data-retry]').onclick=load;}};
 await load();
}
function deletePlaylist(p,current){
 const {modal,close,alive}=dialog('Delete playlist',html`<p>Delete “${esc(p.name)}”? Music files stay available in your library.</p><div class="playlist-actions"><button class="btn" data-cancel>Cancel</button><button class="btn btn-primary" data-confirm>Delete playlist</button></div>`);
 const button=modal.querySelector('[data-confirm]');button.onclick=async()=>{button.disabled=true;try{await API.deletePlaylist(p.playlist_id);if(!current()||!alive())return;close();toast(t('Playlist deleted.'),'success');back();}catch(e){if(!alive()||!current())return;modal.querySelector('[data-error]').textContent=e.message||'Could not delete playlist.';}finally{if(alive())button.disabled=false;}};
}

function matchesSong(song,query){return [mediaTitle(song),song.file_name,song.music?.artist,song.music?.album].join(' ').toLocaleLowerCase().includes(query.trim().toLocaleLowerCase());}
function renderPlaylistDetail(grid,initial,current){
 let p=initial, busy=false;
 let undo=null;
 const meta=html`<details class="playlist-meta"><summary class="btn btn-ghost">Details</summary><p>${accessLabel(p)}</p>${p.comment?html`<p class="playlist-comment">${esc(p.comment)}</p>`:''}${p.rules?html`<p class="playlist-comment" data-rule-summary="${esc(JSON.stringify(p.rules))}">${esc(playlistRulesSummary(p.rules))}</p><p>${ui('Your plays, favorites and ratings use your account. Shared smart playlists can show different songs to each viewer.')}</p>`:''}${p.source?html`<div class="playlist-comment"><p>Following ${esc(p.source.path)} · ${p.source.base==='library'?'Library root':t('Playlist folder')} paths</p><p role="status">${p.source.status==='ready'?`${p.source.unavailable} unavailable · ${p.source.rejected} unsafe entries skipped`:"Source unavailable. Last imported songs retained."}</p><p>Stop following to add, remove or move songs.</p></div>`:''}</details>`;
 grid.innerHTML=html`<div class="grid-full music-heading"><button class="btn btn-ghost" data-back>← ${ui('Back to playlists')}</button><div class="album-header"><div data-playlist-cover></div><div><h1>${esc(p.name)}</h1><p data-totals>${totals(p)}</p>${meta}<div class="playlist-actions">${collectionButtons('Play playlist')}<button class="btn" data-add-songs>Add songs</button><button type="button" class="icon-btn" data-playlist-more aria-label="More" aria-haspopup="menu" title="More">☰</button><span hidden>${favoriteButton('playlist',p.playlist_id,p)}<button class="btn" data-add>Add music</button><button class="btn" data-rules>Rules</button><button class="btn" data-refresh-rules>Refresh results</button><button class="btn" data-edit>Edit playlist</button><button class="btn" data-export>Export M3U8…</button><button class="btn" data-membership>Containing playlists…</button><button class="btn" data-sync>Sync now</button><button class="btn" data-detach>Stop following…</button><button class="btn" data-delete>Delete playlist</button></span></div></div></div><label>Find songs<input type="search" data-find aria-label="Find playlist songs"></label><p role="status" data-count></p><p role="alert" data-song-error></p><div class="playlist-actions" data-undo hidden><span>Song removed from playlist.</span><button class="btn" data-restore>Undo</button></div></div><div class="grid-full" data-rows></div>`;
 mountMusicArtwork(grid.querySelector('[data-playlist-cover]'),'playlist',p.playlist_id,current,cover({...p,artwork_revision:''}));
 const rows=grid.querySelector('[data-rows]'),find=grid.querySelector('[data-find]');
 const draw=(focusID=null)=>{
  state.media=p.songs;
  const visible=p.songs.map((song,index)=>({song,index,entry:p.entries[index]})).filter(({song})=>matchesSong(song,find.value));
  grid.querySelector('[data-totals]').innerHTML=totals(p);
  grid.querySelectorAll('[data-collection-play], [data-collection-shuffle], [data-collection-menu]').forEach(button=>button.disabled=!p.songs.length);
  grid.querySelector('[data-count]').innerHTML=ui('Showing {shown} of {count} songs', {shown: visible.length, count: p.songs.length});
  rows.innerHTML=visible.length?visible.map(({song,index,entry})=>html`<div class="media-row" role="group" tabindex="0" data-id="${song.id}" data-index="${index}" data-entry="${esc(entry.entry_id)}" aria-label="Play ${esc(mediaTitle(song))}"><span class="text-muted music-number">${index+1}</span><span class="music-song"><button class="btn btn-ghost song-open" title="${esc(mediaTitle(song))}">${esc(mediaTitle(song))}</button><span class="text-muted text-sm">${esc(song.music?.artist||t('Unknown artist'))} · ${esc(song.music?.album||'')}</span></span><span>${fmtDur(song.duration)}</span><span hidden>${(p.can_edit===false||p.source||p.rules?[]:[['up',t('Move up')],['down',t('Move down')],['remove','Remove from playlist']]).map(([action,label])=>html`<button data-action="playlist-${action}" ${busy||(action==='up'&&index===0)||(action==='down'&&index===p.songs.length-1)?'disabled':''}>${label}</button>`).join('')}</span><button class="btn btn-ghost" data-song-menu aria-label="Song actions for ${esc(mediaTitle(song))}">⋮</button></div>`).join(''):html`<p>${p.songs.length?'No matching songs. Clear the search to see all songs.':t(p.rules?'No songs match these rules.':'This playlist is empty.')}</p>`;
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
 find.oninput=()=>draw();
 bindCollectionActions(grid.querySelector('[data-collection-actions]'),()=>[{kind:'playlist',key:p.playlist_id}],current,p.name);
 // Collection Shuffle/More fold into the playlist More menu so the strip stays Back/Play/Add songs/Find/More.
 grid.querySelector('[data-collection-shuffle]').hidden=true;
 grid.querySelector('[data-collection-menu]').hidden=true;
 grid.querySelectorAll('[data-add],[data-add-songs],[data-rules],[data-edit],[data-delete]').forEach(button=>button.hidden=p.can_edit===false);
 if(p.rules){grid.querySelectorAll('[data-add],[data-add-songs]').forEach(b=>b.hidden=true);grid.querySelector('[data-refresh-rules]').onclick=reload;}
 else grid.querySelector('[data-refresh-rules]').hidden=true;
 if(p.source){grid.querySelector('[data-rules]').hidden=true;grid.querySelectorAll('[data-add],[data-add-songs]').forEach(b=>b.hidden=true);const sync=grid.querySelector('[data-sync]');if(sync){if(p.owner_id!==state.user?.id)sync.hidden=true;else sync.onclick=()=>changeSource(p,false,current);}grid.querySelector('[data-detach]').onclick=()=>changeSource(p,true,current);}
 else {grid.querySelector('[data-sync]').hidden=true;grid.querySelector('[data-detach]').hidden=true;}
 grid.querySelector('[data-playlist-more]').onclick=event=>playlistDetailMore(event.currentTarget,grid,()=>p,current);
 grid.querySelector('[data-add]').onclick=()=>addMusic(p,current);
 grid.querySelector('[data-add-songs]').onclick=()=>addSongs(p,current);
 grid.querySelector('[data-rules]').onclick=()=>editRules(p,current);
 grid.querySelector('[data-edit]').onclick=()=>editPlaylist(p,current);
 grid.querySelector('[data-export]').onclick=()=>exportPlaylist(p,current);
 grid.querySelector('[data-membership]').onclick=()=>showMembership(p,current);
 grid.querySelector('[data-delete]').onclick=()=>deletePlaylist(p,current);
 grid.querySelector('[data-restore]').onclick=async()=>{const saved=undo;if(!saved)return;if(await save({action:'add',media_ids:[saved.media],before_entry_id:saved.before},'restored')){undo=null;grid.querySelector('[data-undo]').hidden=true;toast('Song restored to playlist.','success');}};
 draw();
 if(pendingRulesID===p.playlist_id){pendingRulesID='';queueMicrotask(()=>{if(current())editRules(p,current,true);});}
}
function playlistDetailMore(button,grid,playlist,current){
 const p=playlist();
 const favorite=grid.querySelector('[data-entity-favorite]');
 const collectionMenu=grid.querySelector('[data-collection-menu]');
 const items=[
  ['favorite',favorite?.getAttribute('aria-label')||t('Favorite playlist'),!favorite],
  ['shuffle',t('Shuffle'),!p.songs.length],
  ['collection',t('Queue and offline…'),!p.songs.length],
  ['add',t('Add music'),grid.querySelector('[data-add]')?.hidden],
  ['rules',t('Rules'),grid.querySelector('[data-rules]')?.hidden],
  ['refresh',t('Refresh results'),grid.querySelector('[data-refresh-rules]')?.hidden],
  ['edit',t('Edit playlist'),grid.querySelector('[data-edit]')?.hidden],
  ['export',t('Export M3U8…'),false],
  ['membership',t('Containing playlists…'),false],
  ['sync',t('Sync now'),grid.querySelector('[data-sync]')?.hidden],
  ['detach',t('Stop following…'),grid.querySelector('[data-detach]')?.hidden],
  ['delete',t('Delete playlist'),grid.querySelector('[data-delete]')?.hidden],
 ].filter(([, , hidden])=>!hidden);
 const popover=document.createElement('div');
 popover.className='shortcut-picker';popover.setAttribute('popover','auto');popover.setAttribute('role','menu');popover.setAttribute('aria-label',t('More playlist actions'));
 popover.innerHTML=items.map(([action,label])=>html`<button class="btn btn-ghost" data-more="${action}">${esc(label)}</button>`).join('');
 const run=action=>{
  popover.hidePopover();
  if(action==='favorite')favorite?.click();
  else if(action==='shuffle')grid.querySelector('[data-collection-shuffle]')?.click();
  else if(action==='collection'){
   const restore=event=>{
    if(event.target?.matches?.('.shortcut-picker') && event.newState==='closed'){
     document.removeEventListener('toggle',restore,true);
     queueMicrotask(()=>{if(button.isConnected)button.focus();});
    }
   };
   document.addEventListener('toggle',restore,true);
   collectionMenu?.click();
  }
  else if(action==='add')grid.querySelector('[data-add]')?.click();
  else if(action==='rules')grid.querySelector('[data-rules]')?.click();
  else if(action==='refresh')grid.querySelector('[data-refresh-rules]')?.click();
  else if(action==='edit')grid.querySelector('[data-edit]')?.click();
  else if(action==='export')grid.querySelector('[data-export]')?.click();
  else if(action==='membership')grid.querySelector('[data-membership]')?.click();
  else if(action==='sync')grid.querySelector('[data-sync]')?.click();
  else if(action==='detach')grid.querySelector('[data-detach]')?.click();
  else if(action==='delete')grid.querySelector('[data-delete]')?.click();
 };
 popover.querySelectorAll('[data-more]').forEach(item=>item.onclick=()=>run(item.dataset.more));
 popover.addEventListener('toggle',event=>{if(event.newState==='closed'){popover.remove();if(button.isConnected)button.focus();}});
 document.body.append(popover);popover.showPopover();popover.querySelector('button')?.focus();
}
function editRules(p,current,enable=Boolean(p.rules)){
 openPlaylistRules(p,{dialog,current,enable,save:API.playlistRules,saved:()=>{toast(t('Playlist rules saved.'),'success');reload();}});
}
async function addSongs(p,current){
 const {modal,close,alive}=dialog('Add songs',html`<p role="status">Loading songs…</p>`);
 const body=modal.querySelector('.modal-body');
 const selected=new Set();
 let saving=false;
 const load=async()=>{
  try{
   const albums=await API.albums();
   const [details,membership]=await Promise.all([Promise.all(albums.map(a=>API.album(a.album_key))),API.playlistMembership()]);
   const targets=new Set([p.playlist_id]);
   if(!current()||!alive())return;
   const songs=[...new Map(details.flatMap(a=>a.songs).map(song=>[song.id,song])).values()];
   body.innerHTML=html`<label>Find songs<input type="search" aria-label="Find songs to add" autofocus></label><label>Repeated songs<select data-duplicates aria-label="Repeated songs"><option value="skip">Skip songs already in each playlist</option><option value="add">Add repeated songs again</option></select></label><details><summary>Add to more playlists</summary><div data-targets></div></details><p role="status" data-selected>0 selected</p><div class="playlist-albums" data-choices></div><p role="alert" data-error></p><div class="playlist-actions"><button class="btn" data-cancel>Cancel</button><button class="btn btn-primary" data-save disabled>Add selected songs</button></div>`;
   body.querySelector('[data-targets]').innerHTML=membership.filter(list=>list.can_edit&&!list.source&&!list.rules&&list.playlist_id!==p.playlist_id).map(list=>html`<label class="playlist-song-choice"><input type="checkbox" data-target="${esc(list.playlist_id)}">${esc(list.name)}</label>`).join('')||html`<p>No other editable playlists.</p>`;
   body.querySelectorAll('[data-target]').forEach(check=>check.onchange=()=>{if(saving)return;check.checked?targets.add(check.dataset.target):targets.delete(check.dataset.target);});
   const input=body.querySelector('input'),choices=body.querySelector('[data-choices]'),submit=body.querySelector('[data-save]');
   const draw=()=>{const matches=songs.filter(song=>matchesSong(song,input.value));choices.innerHTML=matches.length?matches.map(song=>html`<label class="playlist-song-choice"><input type="checkbox" value="${song.id}" ${selected.has(song.id)?'checked':''} ${saving?'disabled':''} aria-label="Select ${esc(mediaTitle(song))}"><span>${esc(mediaTitle(song))}<span class="text-muted text-sm"> · ${esc(song.music?.artist||t('Unknown artist'))} · ${esc(song.music?.album||'')}</span><span class="text-muted text-sm"> · In playlists: ${membership.filter(list=>list.entries?.some(entry=>entry.media_id===song.id)).map(list=>esc(list.name)).join(', ')||'None'}</span></span></label>`).join(''):html`<p>${songs.length?'No matching songs.':'No songs available. Browse Sets or ask an administrator to rescan music tags.'}</p>`;choices.querySelectorAll('input').forEach(check=>check.onchange=()=>{if(saving)return;check.checked?selected.add(Number(check.value)):selected.delete(Number(check.value));body.querySelector('[data-selected]').textContent=`${selected.size} selected`;submit.disabled=saving||!selected.size;});};
   input.oninput=draw;body.querySelector('[data-cancel]').onclick=close;
   submit.onclick=async()=>{if(saving||!selected.size)return;saving=true;submit.disabled=true;body.querySelectorAll('[data-target], [data-duplicates]').forEach(control=>control.disabled=true);choices.querySelectorAll('input').forEach(check=>check.disabled=true);body.querySelector('[data-error]').textContent='';try{await API.addToPlaylists({playlist_ids:[...targets],duplicate_policy:body.querySelector('[data-duplicates]').value,media_ids:songs.filter(song=>selected.has(song.id)).map(song=>song.id)});if(!current()||!alive())return;close();toast('Selected playlists updated.','success');reload();}catch(e){if(current()&&alive())body.querySelector('[data-error]').textContent=e.message||'Could not add songs. Check your connection.';}finally{if(alive()){saving=false;body.querySelectorAll('[data-target], [data-duplicates]').forEach(control=>control.disabled=false);choices.querySelectorAll('input').forEach(check=>check.disabled=false);submit.disabled=!selected.size;}}};
   draw();input.focus();
  }catch(e){if(!current()||!alive())return;body.innerHTML=html`<p role="alert">Could not load songs. Check your connection.</p><button class="btn" data-retry>Retry</button>`;body.querySelector('[data-retry]').onclick=load;}
 };
 await load();
}

function exportPlaylist(p,current){
 const filename=`playlist-${p.playlist_id}.m3u8`;
 const {modal,close,alive}=dialog('Export playlist',html`<p class="playlist-comment">${esc(p.name)}</p><p>UTF-8 M3U8 · <span class="playlist-comment">${esc(filename)}</span></p><p>Paths are relative to the server’s media root. Save this file beside the corresponding library folders in the other tool.</p><p>Only songs you can access with exportable paths are included; repeated songs keep their order. This file does not grant access to music.</p><p role="status" data-export-status></p><div class="playlist-actions"><button class="btn" data-cancel>Cancel</button><button class="btn btn-primary" data-download>Download M3U8</button></div>`);
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

function importPlaylist(current){
 const {modal,close,alive}=dialog('Import M3U',html`<form><label>Server library path<input name="path" required aria-label="Server library path" placeholder="Music/lists/trip.m3u8"></label><p>Choose a server file inside a set you can access. This does not upload a file from this device.</p><label>Paths relative to<select name="base" aria-label="Paths relative to"><option value="folder">Playlist folder</option><option value="library">Library root (Player exports)</option></select></label><label><input type="checkbox" name="follow"> Follow source changes</label><p>One-time import is editable. Following replaces songs on library rescans and when the owner opens playlists; stop following to edit songs. Name, comment and sharing stay manual.</p><div data-summary role="status"></div><div class="playlist-actions"><button class="btn" type="button" data-cancel>Cancel</button><button class="btn btn-primary" type="submit">Preview import</button></div></form>`);
 const form=modal.querySelector('form'),button=form.querySelector('[type=submit]'),summary=form.querySelector('[data-summary]');let preview=null,busy=false;
 const invalidate=()=>{preview=null;summary.textContent='';button.textContent=t('Preview import');};
 form.oninput=invalidate;form.onchange=invalidate;
 form.onsubmit=async event=>{
  event.preventDefault();if(busy)return;busy=true;button.disabled=true;
  const body={path:form.elements.path.value.trim(),base:form.elements.base.value,follow:form.elements.follow.checked,preview:!preview,fingerprint:preview?.fingerprint||''};
  form.querySelectorAll('input,select').forEach(e=>e.disabled=true);modal.querySelector('[data-error]').textContent='';button.textContent=preview?t('Importing…'):t('Reading source…');
  try{const result=await API.importPlaylist(body);if(!alive()||!current())return;if(result.playlist){close();toast(body.follow?'Playlist imported; following source changes.':'Playlist imported.','success');open(result.playlist.playlist_id);return;}preview=result;summary.innerHTML=html`<p class="playlist-comment">${esc(result.name)}</p><p>${result.available} songs · ${result.unavailable} unavailable · ${result.rejected} unsafe entries skipped.</p>${result.unavailable_lines.length?html`<p>Unavailable source lines: ${result.unavailable_lines.join(', ')}${result.unavailable>result.unavailable_lines.length?'…':''}. Missing and inaccessible songs are skipped.</p>`:''}<p>${body.follow?'Ongoing sync: source changes replace this playlist’s songs.':'One-time import: later source changes do not change this playlist.'}</p>`;}
  catch(e){if(alive()&&current()){invalidate();modal.querySelector('[data-error]').textContent=e.message||'Could not import playlist. Check your connection.';}}
  finally{if(alive()){busy=false;button.disabled=false;form.querySelectorAll('input,select').forEach(e=>e.disabled=false);button.textContent=preview?(body.follow?t('Import and follow'):t('Import playlist')):t('Preview import');}}
 };
}
function changeSource(p,detach,current){
 if(detach&&!window.confirm('Stop following this source? Current songs remain and can be edited.'))return;
 const {modal,close,alive}=dialog(detach?'Stop following':'Sync playlist',html`<p role="status">Updating source…</p>`);
 const run=async()=>{try{const result=await API.syncPlaylistSource(p.playlist_id,detach);if(!alive()||!current())return;close();toast(result.source?.status==='unavailable'?'Source unavailable. Last imported songs retained.':detach?'Stopped following source.':'Playlist synchronized.',result.source?.status==='unavailable'?'error':'success');reload();}catch(e){if(alive()&&current()){modal.querySelector('.modal-body').innerHTML=html`<p role="alert"></p><button class="btn" data-retry>Retry</button>`;modal.querySelector('[role=alert]').textContent=e.message||'Could not update source.';modal.querySelector('[data-retry]').onclick=run;}}};run();
}

function bindArtworkFallbacks(grid) {
 grid.querySelectorAll('[data-custom-playlist-art]').forEach(image=>image.addEventListener('error',()=>{
  if(image.dataset.coverFallback)image.src=`/api/media/${image.dataset.coverFallback}/thumbnail`;
  else { const placeholder=document.createElement('div');placeholder.className='album-cover album-placeholder';placeholder.setAttribute('role','img');placeholder.setAttribute('aria-label',t('Artwork unavailable'));placeholder.textContent='♫';image.replaceWith(placeholder); }
 },{once:true}));
}

async function showMembership(p,current){
 const {modal,alive}=dialog('Containing playlists',html`<p role="status">Loading memberships…</p>`);
 const body=modal.querySelector('.modal-body');
 const load=async()=>{try{
  const lists=await API.playlistMembership();if(!current()||!alive())return;
  body.innerHTML=p.songs.length?p.songs.map(song=>html`<p class="playlist-comment"><strong>${esc(mediaTitle(song))}</strong>: ${lists.filter(list=>list.entries?.some(entry=>entry.media_id===song.id)).map(list=>esc(list.name)).join(', ')||'None'}</p>`).join(''):html`<p>This playlist is empty.</p>`;
 }catch(e){if(current()&&alive()){body.innerHTML=html`<p role="alert">Could not load memberships. Check your connection.</p><button class="btn" data-retry>Retry</button>`;body.querySelector('[data-retry]').onclick=load;}}};
 await load();
}

export function playlistDialogContent(title,content) {
 return html`<div class="modal" role="dialog" aria-modal="true" aria-labelledby="playlist-dialog-title"><div class="modal-header"><h2 id="playlist-dialog-title">${ui(title)}</h2><button class="btn btn-ghost" data-close aria-label="Close playlist dialog">✕</button></div><div class="modal-body">${content}<p role="alert" data-error></p></div></div>`;
}

export function playlistEditorContent(p,users=[]) {
 return html`<form><label>Name<input name="name" aria-label="Name" required value="${esc(p?.name||'')}"></label><label>Comment<textarea name="comment" aria-label="Comment" rows="4">${esc(p?.comment||'')}</textarea></label><label>Visibility<select name="visibility" aria-label="Visibility"><option value="private" ${p?.visibility!=='shared'?'selected':''}>Private</option><option value="shared" ${p?.visibility==='shared'?'selected':''}>Shared with server</option></select></label><p>Shared playlists can be read by signed-in accounts on this server. This does not create a public link or grant access to music.</p>${p?.can_transfer?html`<label>Owner<select name="owner" aria-label="Owner">${users.map(u=>html`<option value="${u.id}" ${u.id===p.owner_id?'selected':''}>${esc(u.username)}</option>`).join('')}</select></label>`:''}${p?.source&&p?.can_transfer?html`<p>Changing owner stops following the source and keeps the current songs.</p>`:''}<p class="text-muted">Name: 1–200 characters. Comment: up to 2000 characters.</p><div class="playlist-actions"><button type="button" class="btn" data-cancel>Cancel</button><button class="btn btn-primary" type="submit">${ui(p?'Save changes':'Create playlist')}</button></div></form>`;
}
