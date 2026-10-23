import { state } from './state.js';
import { offlineSavedCollections } from './offline-pins.js';
import { showOfflinePin } from './offline-pin-dialog.js';

export function initOfflinePinLibrary() {
 const host=document.getElementById('settings-modal')?.querySelector('.modal');
 if(!host || host.querySelector('[data-offline-library]'))return;
 const panel=document.createElement('section');panel.dataset.offlineLibrary='';
 panel.innerHTML='<h4>Saved music collections</h4><p><a class="btn" href="/index.html?offline=music">Browse offline music</a></p><p>Pinned album and playlist copies stay separate from disposable playback cache. Browser storage is best effort; clearing site data removes downloads.</p><p role="status" data-status></p><button class="btn" data-refresh>Refresh saved collections</button><details><summary>Manage downloaded collections</summary><ul data-collections></ul></details>';
 host.append(panel);
 let revision=0;
 async function render() {
  const version=++revision, account=state.user, status=panel.querySelector('[data-status]'),list=panel.querySelector('ul');
  list.replaceChildren();
  if(!account) {status.textContent='Sign in online to unlock this account’s saved collections. Offline sign-in is not available.';return;}
  status.textContent='Loading saved collections…';
  try {
   const rows=await offlineSavedCollections();if(version!==revision || state.user!==account)return;
   status.textContent=rows.length?`${rows.length} saved collections`:'No saved collections. Open an album or playlist and choose Pin offline.';
   for(const row of rows) {
    const li=document.createElement('li'),button=document.createElement('button');button.className='btn btn-ghost';
    button.textContent=`${row.title} · ${row.pinned?'Pinned':'Unpinned'} · ${row.songs.length}/${row.urls.length} songs downloaded`;
    button.onclick=()=>showOfflinePin(row.collection,row.title,button);li.append(button);list.append(li);
   }
  } catch {if(version===revision)status.textContent='Could not read saved downloads. Check browser storage and retry.';}
 }
 panel.querySelector('[data-refresh]').onclick=render;
 document.addEventListener('offline-pins:changed',render);
 document.addEventListener('playback-cache:changed',render);
 window.addEventListener('storage',event=>{if(event.key?.startsWith('player-offline-v1-'))void render();});
 void render();
}
