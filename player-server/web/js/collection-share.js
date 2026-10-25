import { shareQualityEditor, bindShareQuality } from './share-quality.js';
import { state } from './state.js';
import { copyShareLink } from './share-link.js';
import { escapeHtml as esc } from './utils.js';

// Captures occurrences once; changing selection never changes a pending share.
export function shareMusicCollection(songs, title = 'Selected songs', opener = document.activeElement, single = false) {
 const ids = songs.map(song => song.id), user = state.user;
 const overlay = document.createElement('div'); overlay.className = 'modal-overlay open';
 overlay.innerHTML = `<section class="modal" role="dialog" aria-modal="true" aria-labelledby="collection-share-title"><div class="modal-header"><h2 id="collection-share-title">Share music</h2></div><form class="modal-body"><p>${esc(title)} · ${single ? '1 file' : `${ids.length} songs`}</p><p>${single ? 'Anyone with the link can play and download this file.' : 'This link contains a fixed snapshot in this order. Later collection edits do not change it. Anyone with the link can play and download these songs.'}</p>${songs.every(song => song.type === 'audio') ? shareQualityEditor() : ''}<label>Expires after <input type="date" name="expiry" required aria-label="Expiry date"></label><label>Maximum viewings (optional)<input name="uses" type="number" min="1" step="1" aria-label="Maximum viewings"></label><p role="status" data-status></p><label hidden data-result>Share link<input readonly aria-label="Share link"></label><div class="playlist-actions"><button type="button" class="btn" data-close>Close</button><button class="btn btn-primary" type="submit">Create link</button><button type="button" class="btn" data-copy hidden>Copy link</button></div></form></section>`;
 const form=overlay.querySelector('form'),status=overlay.querySelector('[data-status]'), submit=overlay.querySelector('[type=submit]'),result=overlay.querySelector('[data-result] input'),expiry=form.elements.expiry;
 const setConfigurationDisabled=disabled=>form.querySelectorAll('input:not([readonly]),select,[data-reload]').forEach(control=>{control.disabled=disabled;});
 const day=new Date(); day.setDate(day.getDate()+7); expiry.value=`${day.getFullYear()}-${String(day.getMonth()+1).padStart(2,'0')}-${String(day.getDate()).padStart(2,'0')}`;
 let closed=false,url='';
 const close=()=>{closed=true;overlay.remove();if(opener?.isConnected)opener.focus();};
 overlay.querySelector('[data-close]').onclick=close;
 overlay.addEventListener('keydown',event=>{ if(event.key==='Escape'){event.preventDefault();event.stopPropagation();close();} });
 overlay.querySelector('[data-copy]').onclick=()=>copyShareLink(url);
 submit.disabled=!ids.length||ids.length>1000;
 if(submit.disabled)status.textContent='Select between 1 and 1000 songs.';
 form.onsubmit=async event=>{
  event.preventDefault(); if(submit.disabled)return;
  const until=new Date(`${expiry.value}T00:00:00`);until.setDate(until.getDate()+1);
  if(!Number.isFinite(until.getTime())||until<=new Date()){status.textContent='Choose a future expiry.';return;}
  const max=form.elements.uses.value?Number(form.elements.uses.value):null;
  if(max!==null&&(!Number.isSafeInteger(max)||max<1)){status.textContent='Maximum viewings must be a positive whole number.';return;}
  submit.disabled=true;setConfigurationDisabled(true);status.textContent='Creating link…';
  try {
   if(state.user!==user)throw Error('Account changed. Close this dialog and try again.');
   const response=await fetch(single ? `/api/media/${ids[0]}/shares` : '/api/shares/collections',{method:'POST',credentials:'include',headers:{'Content-Type':'application/json'},body:JSON.stringify({...(single ? {} : {title:String(title).slice(0,200),media_ids:ids}),expires_at:until.toISOString(),max_uses:max,audio_quality:form.elements.audio_quality?.value || ''})});
   if(!response.ok)throw Error('Could not create link. Check your connection and access, then retry.');
   const share=await response.json();if(closed||state.user!==user)return;
   url=`${location.origin}/s/${encodeURIComponent(share.token)}`;result.value=url;overlay.querySelector('[data-result]').hidden=false;overlay.querySelector('[data-copy]').hidden=false;submit.hidden=true;status.textContent='Link ready. Manage or revoke it in My Shares.';result.focus();result.select();
  } catch(error){if(!closed){status.textContent=error.message;submit.disabled=false;setConfigurationDisabled(false);}}
 };
 document.body.append(overlay);bindShareQuality(overlay);expiry.focus();
}
