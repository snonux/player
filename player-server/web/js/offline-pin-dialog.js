import { initDownloadQuality } from './download-quality.js';
import { beginPlaybackIntent, isPlaybackIntent } from './playback-intent.js';
import { API } from './api.js';
import { state } from './state.js';
import { focusSelectedCard } from './selection.js';
import { resolveCollections } from './collection-resolver.js';
import { insertQueue } from './queue.js';
import { offlinePinStatus, pinMusicCollection, unpinMusicCollection } from './offline-pins.js';

export function showOfflinePin(collection, title, opener = document.activeElement) {
 const account = state.user;
 const overlay=document.createElement('div'); overlay.className='modal-overlay open';
 overlay.innerHTML='<section class="modal offline-pin-modal" role="dialog" aria-modal="true" aria-labelledby="offline-pin-title"><div class="modal-header"><h2 id="offline-pin-title" tabindex="-1"></h2></div><div class="modal-body"><p data-status role="status" aria-live="polite">Loading saved collection…</p><p>Unpin keeps downloads. Remove download deletes this browser’s copies that no other saved collection uses. Server music stays available.</p><label>Download quality <select data-download-quality aria-label="Download quality"></select></label><p>New copies use this quality; existing copies keep theirs. Original files need a compatible browser codec.</p><p data-quality-status role="status"></p><button class="btn" data-quality-reload>Reload download profiles</button><p data-storage>Browser storage may be reclaimed or cleared. Keep this tab open while saving.</p><div class="playlist-actions"><button class="btn btn-primary" data-pin>Pin offline</button><button class="btn" data-play>Play downloaded</button><button class="btn" data-unpin>Unpin</button><button class="btn" data-remove>Remove download…</button><button class="btn" data-close>Close</button></div></div></section>';
 overlay.querySelector('h2').textContent=title;
 const status=overlay.querySelector('[data-status]'), pin=overlay.querySelector('[data-pin]'), play=overlay.querySelector('[data-play]'), unpin=overlay.querySelector('[data-unpin]'), remove=overlay.querySelector('[data-remove]'), close=overlay.querySelector('[data-close]');
 let row=null, busy=false, closed=false, controller=null;
 const current=()=>!closed && state.user === account;
 function render(message) {
  status.textContent=message || (row ? `${row.pinned?'Pinned':'Unpinned'} · ${row.songs.length} of ${row.urls.length} songs downloaded${!row.complete?' · Retry pinning to finish.':''}${row.songs.length ? ` · Saved quality: ${[...new Set(row.songs.map(song=>({original:'Original',aac128:'AAC 128 kbps',aac256:'AAC 256 kbps',mp3192:'MP3 192 kbps',opus96:'Opus 96 kbps'}[song.profile] || 'Existing copy (quality not recorded)') + (song.contentType ? ` (${song.contentType})` : '')))].join(', ')}` : ''}` : 'Not pinned');
  overlay.querySelector('[data-download-quality]').disabled=busy;
  pin.disabled=busy || !current() || !globalThis.caches || Boolean(row?.automatic);
  play.disabled=busy || !row?.complete || !current(); unpin.disabled=busy || !row?.pinned || !current(); remove.disabled=busy || !row || !current();
  pin.textContent=row?.automatic ? 'Managed in Automatic offline music' : row ? row.complete && row.pinned ? 'Refresh pin' : 'Retry pinning' : 'Pin offline';
  close.textContent=busy?'Cancel and close':'Close';
 }
 async function refresh() { row=await offlinePinStatus(collection); if(current())render(); }
 function finish() {
  if(closed)return;
  closed=true; controller?.abort(); overlay.remove();
  if(state.user !== account || document.querySelector('.modal-overlay.open'))return;
  if(opener?.isConnected && !opener.disabled && opener.getClientRects().length)opener.focus();
  else focusSelectedCard();
 }
 close.onclick=finish;
 overlay.onkeydown=event=>{if(event.key==='Escape'){event.preventDefault();event.stopPropagation();finish();}};
 overlay.addEventListener('modalbeforeclose',event=>{event.preventDefault();finish();});
 async function run(action) {
  if(busy || !current())return;
  busy=true;controller=new AbortController(); render('Loading songs…');
  if(document.activeElement===document.body)close.focus();
  const timer=setInterval(()=>{if(!current())controller.abort();},200);
  try { await action(); if(current())await refresh(); }
  catch(error) { if(current()) { try { row=await offlinePinStatus(collection); } catch {} render(error.name==='AbortError'?'Download canceled. Completed files remain available.':`${error.message} Completed files remain available; retry when ready.`); } }
  finally { clearInterval(timer);busy=false; if(current()) { const message=status.textContent;render(message); } }
 }
 pin.onclick=()=>{ const profile=overlay.querySelector('[data-download-quality]').value; return run(async()=>{
  if(navigator.storage?.persist) {
   try {
    // Firefox can leave this permission request pending while its prompt is
    // unanswered. Continue saving; persistence is optional for CacheStorage.
    const persistent=await Promise.race([navigator.storage.persist(),new Promise(resolve=>setTimeout(()=>resolve(null),1500))]);
    if(current())overlay.querySelector('[data-storage]').textContent=persistent===true?'Persistent storage granted. Clearing site data still removes downloads.':persistent===false?'Persistent storage was not granted. Your browser may reclaim downloads; keep exported backups.':'Storage permission is pending. Saving now; your browser may reclaim downloads.';
   } catch { /* CacheStorage may still be usable without persistence. */ }
  }
  const songs=await resolveCollections(API,[collection],current);
  if(!current())return;
  await pinMusicCollection(collection,title,songs,{signal:controller.signal,profile,progress:(done,total)=>{if(current())render(`Saving ${done} of ${total} songs…`);}});
 });};
 unpin.onclick=()=>run(()=>unpinMusicCollection(collection));
 remove.onclick=()=>{if(confirm('Remove this collection’s downloads from this browser? Files used by other saved collections and server music stay available.'))void run(()=>unpinMusicCollection(collection,{remove:true}));};
 play.onclick=()=>run(async()=>{
  const intent=beginPlaybackIntent();
  const saved=await offlinePinStatus(collection);
  if(!saved?.complete)throw Error('Some downloads are missing. Retry pinning first.');
  if(!current() || !isPlaybackIntent(intent))return;
  const songs=saved.songs.map(row=>({...row.media,offlinePinURL:row.url}));
  if(await insertQueue(songs,'now',{replace:true,source:`Downloaded ${title}`}))finish();
  else throw Error('Playback is busy. Retry.');
 });
 document.body.append(overlay);initDownloadQuality(overlay);overlay.querySelector('h2').focus();render('Loading saved collection…');
 refresh().catch(()=>{if(current())render('Could not read saved downloads. Check browser storage and retry.');});
}

// Collection selection owners call update after changing their selection. The
// durable state stays on this collection control as well as in Settings.
export function bindOfflinePinButton(button, collections, current, title) {
 if (!button) return async () => {};
 let revision=0;
 async function update() {
  const version=++revision, selected=collections();
  button.disabled=selected.length !== 1 || !['album','playlist'].includes(selected[0]?.kind);
  button.textContent='Pin offline…';
  if(button.disabled)return;
  try {
   const saved=await offlinePinStatus(selected[0]);
   if(version!==revision || !button.isConnected || !current())return;
   if(saved)button.textContent=`${saved.pinned?'Pinned':'Downloaded'} ${saved.songs.length}/${saved.urls.length} · Manage…`;
  } catch { /* Dialog provides the retryable storage error. */ }
 }
 button.onclick=()=>{const selected=collections();if(current() && selected.length===1 && ['album','playlist'].includes(selected[0].kind))showOfflinePin(selected[0],title,button);};
 document.addEventListener('offline-pins:changed',()=>{if(button.isConnected && current())void update();});
 void update();return update;
}
