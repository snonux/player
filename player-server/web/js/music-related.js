import { mountMusicMix } from './music-mix.js';
import { API } from './api.js';
import { escapeHtml as esc } from './utils.js';
import { resolveCollections } from './collection-resolver.js';
import { insertQueue, queueState } from './queue.js';
import { beginPlaybackIntent, isPlaybackIntent } from './playback-intent.js';
import { state } from './state.js';

// Independent native buttons leave catalog selection and its keyboard scope intact.
export function mountRelatedMusic(host, kind, key, current, open) {
 mountMusicMix(host, kind, key, current);
 const section=document.createElement('section');
 section.className='grid-full music-related'; section.setAttribute('aria-label','More like this');
 host.append(section);
 const alive=()=>section.isConnected && current();
 async function load() {
  section.innerHTML='<h2>More like this</h2><p role="status">Finding local metadata matches…</p>';
  try {
   const items=await API.relatedMusic(kind,key);
   if(!alive())return;
   section.innerHTML='<h2>More like this</h2><p class="text-muted">From your library · shared music tags</p>'+(items.length?'<div class="music-related-list">'+items.map((item,index)=>`<article class="card-body">${item.cover_media_id?`<img class="album-cover" src="/api/media/${Number(item.cover_media_id)}/thumbnail" alt="" loading="lazy">`:""}<h3>${esc(item.title)}</h3><p class="text-muted">${item.basis.map(esc).join(' · ')}</p><button class="btn btn-ghost" data-related-open="${index}" aria-label="Open ${esc(kind)} ${esc(item.title)}">Open ${esc(kind)}</button> <button class="btn" data-related-play="${index}" aria-label="Play ${esc(item.title)}">▶ Play</button><span role="status" data-related-status="${index}"></span></article>`).join('')+'</div>':'<p>No related music found. Add genre or artist tags to find more matches.</p>');
   section.querySelectorAll('img').forEach(img=>img.addEventListener('error',()=>img.remove(),{once:true}));
   section.querySelectorAll('[data-related-open]').forEach(button=>button.onclick=()=>open(items[Number(button.dataset.relatedOpen)]));
   section.querySelectorAll('[data-related-play]').forEach(button=>button.onclick=async()=>{
    const index=Number(button.dataset.relatedPlay), item=items[index], status=section.querySelector(`[data-related-status="${index}"]`);
    if(queueState().loading) { status.textContent='Loading track… Please wait.'; return; }
    const user=state.user,intent=beginPlaybackIntent();
    const active=()=>alive() && state.user===user && isPlaybackIntent(intent);
    button.disabled=true;status.textContent='Loading songs…';
    try {
     const songs=item.kind==='song'?[item.song]:await resolveCollections(API,[{kind:item.kind,key:item.key}],active);
     if(!active())return;
     const started=await insertQueue(songs,'now',{replace:true,source:item.title});
     if(alive())status.textContent=started?'':'Playback was not started. Try again.';
    } catch(error) {if(alive())status.textContent=`Could not play. ${error.message || 'Check your connection.'}`;}
    finally {if(alive())button.disabled=false;}
   });
  } catch(error) {
   if(!alive())return;
   section.innerHTML=`<h2>More like this</h2><p role="alert">Could not load related music. ${esc(error.message || 'Check your connection.')}</p><button class="btn" data-related-retry>Retry</button>`;
   section.querySelector('button').onclick=load;
  }
 }
 void load();
 return section;
}
