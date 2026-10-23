import { offlineCachedAccounts, offlineCachedSongs } from './offline-cache.js';
import { t, html } from './i18n.js';
import { offlineMusicAccount, selectOfflineMusicAccount } from './offline-context.js';
import { offlineSavedAccounts, offlineSavedCollections, offlineSavedSongs } from './offline-pins.js';
import { offlineMusicGroups, offlineMusicMatches } from './offline-browse-model.js';
import { mediaTitle, musicSubtitle } from './music.js';
import { initPlayer, stopAndClose, togglePlay } from './player.js';
import { initQueue, insertQueue, advanceQueue, resetOfflineQueue, shuffleQueue, cycleQueueRepeat } from './queue.js';
import { initQueuePanel, toggleQueue, isQueueFocused, queueKey } from './queue-panel.js';
import { releasePlaybackCache } from './playback-cache.js';
import { beginPlaybackIntent, isPlaybackIntent } from './playback-intent.js';

// Existing playback chrome and queue serve local downloads; no authenticated
// identity, API initialization or online collection resolver is involved.
export function initOfflineMusicApp() {
 const header=document.querySelector('.header-main'), host=document.querySelector('.main-content');
 const shortcuts=document.getElementById('keyboard-access-toggle');
 header.innerHTML=html`<a href="/index.html" class="logo">Player</a><div class="spacer"></div><button class="btn" data-queue-toggle>Queue</button><button id="theme-toggle" class="icon-btn" aria-label="Toggle theme"></button><a class="btn" href="/login.html">Sign In</a>`;
 if(shortcuts)header.append(shortcuts);
 document.querySelector('#sidebar')?.classList.remove('open');
 document.querySelector('.page')?.classList.remove('has-sidebar');
 host.innerHTML=html`<section><h1>Offline music</h1><p>Saved on this device. Online actions need a connection and sign-in.</p><p>Cached copies are disposable and may be evicted. Downloads stay separate.</p><p data-origin></p><div class="music-facet-controls"><label>Saved server and account <select data-account></select></label><label>Browse <select data-kind><option value="Songs">Songs</option><option value="Albums">Albums</option><option value="Artists">Artists</option><option value="Playlists">Playlists</option></select></label><label>Search downloaded music <input type="search" data-search></label><button class="btn" data-refresh>Refresh saved collections</button></div><p role="status" aria-live="polite" data-status></p><p>j/k or arrows select · Enter open · / search · Esc back · p play/pause · Q queue</p><div data-results></div></section>`;
 host.querySelector('[data-origin]').textContent=location.origin;
 initPlayer({personalActions:false,cast:false,onNext:options=>advanceQueue(options?.delta ?? 1,options),onPrevious:options=>advanceQueue(options?.delta ?? -1,options)});
 initQueue();initQueuePanel();
 // Detach uses network library playback; local-only mode stays in this tab.
 for (const selector of ['#btn-detach','#btn-cast','#btn-download','#btn-player-notes','#btn-player-favorite','#btn-bookmarks','#btn-save-position']) {
  const control=document.querySelector(selector);if(control){control.disabled=true;control.hidden=true;}
 }
 const account=host.querySelector('[data-account]'),kind=host.querySelector('[data-kind]'),search=host.querySelector('[data-search]'),status=host.querySelector('[data-status]'),results=host.querySelector('[data-results]');
 let revision=0,songs=[],collections=[],detail=null,selected=0,busy=false;
 async function savedSongs() {
  const [downloads,cached]=await Promise.all([offlineSavedSongs(),offlineCachedSongs()]);
  const seen=new Set(downloads.map(row=>row.media.id));
  return [...downloads.map(row=>({...row,storageKind:'download'})),...cached.filter(row=>!seen.has(row.media.id))];
 }
 function selectedRows() {return offlineMusicGroups(songs,collections,kind.value).filter(row=>offlineMusicMatches(row,search.value));}
 function message(text) {status.textContent=t(text);}
 function button(label, action) {const b=document.createElement('button');b.className='btn';b.textContent=t(label);b.onclick=action;return b;}
 function render() {
  results.replaceChildren();
  if(detail) {
   results.append(button('Back',()=>{detail=null;render();focusRow();}));
   const heading=document.createElement('h2');heading.textContent=detail.title;results.append(heading);
   const count=document.createElement('p');count.textContent=t('{saved} of {total} songs available',{saved:detail.songs.length,total:detail.total});results.append(count);
   if(detail.songs.length<detail.total) {const missing=document.createElement('p');missing.textContent=t('Some downloads are missing. Reconnect and download them again.');results.append(missing);}
   for(const [label,action] of [['Play now','now'],['Play next','next'],['Add to queue','append']]) {
    const b=button(label,()=>play(detail.songs,action));b.disabled=busy || !detail.songs.length;results.append(b);
   }
   const list=document.createElement('ul');
   for(const song of detail.songs) {const li=document.createElement('li'), b=button('',()=>play([song],'now'));b.textContent=[mediaTitle(song.media),musicSubtitle(song.media)].filter(Boolean).join(' · ');b.disabled=busy;const label=document.createElement('small');label.textContent=t(song.storageKind==='cache'?'Cached (disposable)':'Downloaded');li.append(b,label);list.append(li);}
   results.append(list);return;
  }
  const rows=selectedRows();selected=Math.max(0,Math.min(selected,rows.length-1));
  if(!rows.length){message('No downloaded music matches.');return;}
  rows.forEach((row,i)=>{
   const item=document.createElement('div'),open=button('',()=>{selected=i;detail=row;render();results.querySelector('button')?.focus();});
   open.dataset.offlineRow=String(i);open.textContent=row.title;
   const count=document.createElement('p');count.textContent=t('{saved} of {total} songs available',{saved:row.songs.length,total:row.total});
   item.append(open,count);results.append(item);
  });
 }
 function focusRow() {results.querySelector(`[data-offline-row="${selected}"]`)?.focus();}
 async function play(rows,action) {
  if(busy || !rows.length)return;
  const epoch=revision,id=offlineMusicAccount(),intent=beginPlaybackIntent();busy=true;render();
  try {
   // Re-read bytes immediately before queueing. Missing files never stream.
   const available=new Set((await savedSongs()).map(song=>song.url));
   if(epoch!==revision || id!==offlineMusicAccount() || !isPlaybackIntent(intent))return;
   if(rows.some(song=>!available.has(song.url)))throw Error();
   const added=await insertQueue(rows.map(song=>({...song.media,offlinePinURL:song.url})) ,action,{replace:action==='now',source:t('Offline music')});
   if(epoch===revision)message(added?'Added to queue':'Playback is busy. Retry.');
  } catch {if(epoch===revision)message('Some downloads are missing. Reconnect and download them again.');}
  finally {busy=false;if(epoch===revision){render();results.querySelector('button')?.focus();}}
 }
 async function load() {
  const epoch=++revision;detail=null;selected=0;message('Loading saved collections…');
  try {
   const loaded=await Promise.all([savedSongs(),offlineSavedCollections()]);
   if(epoch!==revision)return;[songs,collections]=loaded;collections=collections.map(row=>({...row,songs:row.urls.map(url=>row.songs.find(song=>song.url===url) || songs.find(song=>String(song.media.id)===new URL(url,location.origin).pathname.match(/\/media\/(\d+)\//)?.[1])).filter(Boolean)}));message('Saved on this device. Online actions need a connection and sign-in.');render();
  } catch {if(epoch===revision){songs=[];collections=[];results.replaceChildren();message('Could not read saved downloads. Check browser storage and retry.');}}
 }
 function changeAccount() {
  revision++;stopAndClose();resetOfflineQueue();releasePlaybackCache();
  selectOfflineMusicAccount(Number(account.value));songs=[];collections=[];void load();
 }
 account.onchange=changeAccount;kind.onchange=()=>{detail=null;selected=0;render();};search.oninput=()=>{detail=null;selected=0;render();};
 host.querySelector('[data-refresh]').onclick=load;
 document.addEventListener('offline-pins:changed',()=>void load());
 window.addEventListener('storage',event=>{if(event.key?.startsWith('player-offline-v1-'))void load();});
 document.addEventListener('keydown',event=>{
  if(event.ctrlKey || event.metaKey || event.altKey || event.isComposing)return;
  if(event.target.matches('input,textarea,select') || event.target.isContentEditable){if(event.key==='Escape')event.target.blur();return;}
  if(isQueueFocused()){queueKey(event);return;}
  if(document.querySelector('.modal-overlay.open'))return;
  if(event.key==='/' ){event.preventDefault();search.focus();return;}
  if(['j','k','ArrowDown','ArrowUp','h','l','ArrowLeft','ArrowRight'].includes(event.key) && !detail){event.preventDefault();selected=Math.max(0,Math.min(selectedRows().length-1,selected+(['j','l','ArrowDown','ArrowRight'].includes(event.key)?1:-1)));focusRow();}
  else if(event.key==='Escape' || event.key==='Backspace'){if(detail){event.preventDefault();detail=null;render();focusRow();}}
  else if(event.key==='p' || event.key===' '){event.preventDefault();togglePlay();}
  else if(event.key==='Q'){event.preventDefault();toggleQueue();}
  else if(event.key==='r'){event.preventDefault();void shuffleQueue();}
  else if(event.key==='Y'){event.preventDefault();cycleQueueRepeat();}
 });
 try {
  const ids=[...new Set([...offlineSavedAccounts(),...offlineCachedAccounts()])].sort((a,b)=>a-b);
  for(const id of ids){const option=document.createElement('option');option.value=String(id);option.textContent=t('Account {id}',{id});account.append(option);}
  if(!ids.length){account.disabled=true;message('No downloaded music yet.');return;}
  changeAccount();
 } catch {message('Could not read saved downloads. Check browser storage and retry.');}
}
