import { API } from './api.js';
import { escapeHtml as esc } from './utils.js';

// Metadata stays secondary to playback and never replaces local artwork.
export function mountMusicEnrichment(host, kind, key, current) {
 const section=document.createElement('section');
 section.className='grid-full music-enrichment';section.hidden=true;
 const toggle=document.createElement('button'); toggle.className='btn btn-ghost';
 toggle.textContent='About'; toggle.setAttribute('aria-expanded','false');
 const body=document.createElement('div'); body.hidden=true;
 const content=document.createElement('div'); content.setAttribute('role','status');
 const refresh=document.createElement('button');refresh.className='btn btn-ghost';refresh.textContent='Refresh metadata';refresh.hidden=true;
 body.append(content,refresh);section.append(toggle,body);host.append(section);
 let loaded=false, busy=false;
 const alive=()=>section.isConnected && current();
 toggle.onclick=()=>{body.hidden=!body.hidden;toggle.setAttribute('aria-expanded',String(!body.hidden));if(!body.hidden && !loaded)void load();};
 refresh.onclick=()=>void load(true);
 async function load(force=false) {
  if(busy)return;busy=true;refresh.setAttribute('aria-disabled','true');
  if(force)refresh.textContent='Refreshing metadata…';
  content.setAttribute('aria-busy','true');
  if(!loaded)content.textContent='Loading metadata…';
  try {
   const data=await API.musicEnrichment(kind,key,force);
   if(!alive())return;
   loaded=true;
   // Disabled providers quietly remove the optional section.
   if(!data.enabled){section.remove();return;}
   body.querySelector('[data-enrichment-error]')?.remove();
   refresh.onclick=()=>void load(true);refresh.textContent='Refresh metadata';
   section.hidden=false;refresh.hidden=!data.can_refresh;
   content.innerHTML=(data.image_url?`<img class="album-cover" src="${esc(data.image_url)}" alt="${esc(kind==='artist'?'Artist image from '+data.source:'Metadata image from '+data.source)}" loading="lazy" referrerpolicy="no-referrer">`:'')+
    (data.description?`<p class="music-biography">${esc(data.description)}</p>`:'')+
    (data.links?.length?`<ul>${data.links.map(link=>`<li><a href="${esc(link.url)}" target="_blank" rel="noopener noreferrer">${esc(link.label)} ↗</a></li>`).join('')}</ul>`:'')+
    (data.source?`<p class="text-secondary">Source: ${esc(data.source)}${data.fetched_at?' · Updated '+esc(new Date(data.fetched_at).toLocaleString()):''}${data.stale?' · May be out of date':''}</p>`:'')+
    (!data.source && data.fetched_at?`<p>Last checked: ${esc(new Date(data.fetched_at).toLocaleString())}</p>`:'')+
    (data.state==='empty'?'<p>No metadata found for this artist or album.</p>':'')+
    (data.state==='unfetched'?`<p>${data.can_refresh?'Choose Refresh metadata to look up this artist or album.':'No metadata fetched yet. An administrator can refresh it.'}</p>`:'')+
    (data.state==='error'?`<p>${esc(data.message)}</p>`:'');
   content.querySelector('img')?.addEventListener('error',event=>{const message=document.createElement('p');message.textContent='Provider image is unavailable. Local artwork is unchanged.';event.target.replaceWith(message);},{once:true});
  } catch(error) {
   if(!alive())return;
   section.hidden=false;
   if(!loaded)content.textContent='';
   if([401,403,404].includes(error.status)){content.textContent='Metadata is unavailable or you no longer have permission.';loaded=false;}
   // Keep already rendered data through connection failures.
   let failure=body.querySelector('[data-enrichment-error]');
   if(!failure){failure=document.createElement('p');failure.dataset.enrichmentError='';body.append(failure);}
   failure.textContent=`Could not load metadata. ${error.message || 'Check your connection and try again.'}`;
   refresh.hidden=false;refresh.textContent=loaded?'Refresh metadata':'Retry';
   if(!loaded)refresh.onclick=()=>void load();
  } finally {
   if(alive()){busy=false;refresh.setAttribute('aria-disabled','false');if(refresh.textContent==='Refreshing metadata…')refresh.textContent='Refresh metadata';content.removeAttribute('aria-busy');}
  }
 }
 // Discover availability without fetching external metadata, then leave About closed.
 void load();
 return section;
}
