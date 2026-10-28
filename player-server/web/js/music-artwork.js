import { escapeHtml as esc } from './utils.js';
import { redirectToLogin } from './api.js';

const path=(kind,key)=>`/api/music/artwork/${kind}/${encodeURIComponent(key)}`;
async function request(kind,key,options={}) {
 const response=await fetch(path(kind,key),{credentials:'include',cache:'no-store',...options});
 if(response.status===401)redirectToLogin();
 const data=await response.json();
 if(!response.ok){const error=new Error(data.error || 'Artwork unavailable.');error.status=response.status;throw error;}
 return data;
}
// One stable preview slot. Never replace a saved preview until a write succeeds.
export function mountMusicArtwork(host,kind,key,current,fallback='',options={}) {
 const section=document.createElement('section');section.className='music-artwork';
 section.setAttribute('aria-label',`${kind} artwork`);
 section.innerHTML='<div data-art-preview></div><div data-art-controls></div><p role="status" data-art-status>Loading artwork…</p>';
 host.prepend(section);
 const preview=section.querySelector('[data-art-preview]'),controls=section.querySelector('[data-art-controls]'),status=section.querySelector('[data-art-status]');
 let data=null,busy=false;
 const alive=()=>section.isConnected && current();
 const original=fallback || `<div class="album-cover album-placeholder" role="img" aria-label="No ${kind} artwork">♫</div>`;
 preview.innerHTML=original;
 function draw() {
  preview.innerHTML=data.has_artwork?`<img class="album-cover" src="${esc(path(kind,key))}/image?revision=${encodeURIComponent(data.revision)}" alt="Custom ${kind} artwork">`:original;
  preview.querySelector('img')?.addEventListener('error',()=>{if(alive())status.textContent='Artwork could not be loaded. Retry or check your connection.';});
  controls.innerHTML=data.can_edit?'<button class="btn" data-art-change>Change artwork…</button> <button class="btn btn-ghost" data-art-remove>Remove artwork</button>':'';
  controls.hidden=Boolean(options.menuOnly);
  if(!data.can_edit)return;
  const change=controls.querySelector('[data-art-change]'),remove=controls.querySelector('[data-art-remove]');remove.hidden=!data.has_artwork;
  change.onclick=()=>{if(busy)return;const input=document.createElement('input');input.type='file';input.accept='image/png,image/jpeg,image/gif';input.onchange=()=>{const file=input.files?.[0];if(file)void save(file);};input.click();};
  remove.onclick=()=>{if(!busy && confirm('Remove custom artwork and restore the default cover?'))void save(null);};
 }
 async function load(){try{const next=await request(kind,key);if(!alive())return;data=next;draw();status.textContent=data.can_edit?'JPEG, PNG or GIF · up to 5 MB.':'';}catch(error){if(!alive())return;if([401,403,404].includes(error.status)){preview.innerHTML='';controls.innerHTML='';}status.textContent=error.message;const retry=document.createElement('button');retry.className='btn';retry.textContent='Retry artwork';retry.onclick=()=>void load();controls.append(retry);}}
 async function save(file){
  if(!alive() || busy)return;
  if(file?.size>5*1024*1024){status.textContent='Choose an image up to 5 MB.';return;}
  busy=true;controls.setAttribute('aria-busy','true');status.textContent='Saving artwork…';
  try{const next=await request(kind,key,{method:file?'PUT':'DELETE',body:file || undefined,headers:file?{'Content-Type':'application/octet-stream'}:{}});if(!alive())return;data=next;draw();status.textContent=file?'Artwork saved.':'Custom artwork removed.';controls.querySelector('[data-art-change]')?.focus();}
  catch(error){if(!alive())return;if([401,403,404].includes(error.status)){preview.innerHTML='';controls.innerHTML='';}status.textContent=error.message;}
  finally{if(alive()){busy=false;controls.removeAttribute('aria-busy');}}
 }
 void load();return section;
}
