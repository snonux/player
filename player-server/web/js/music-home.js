import { t, html, plural } from './i18n.js';
import { state } from './state.js';
import { API } from './api.js';
import { escapeHtml as esc, toast } from './utils.js';
import { collectionButtons, bindCollectionActions } from './collection-actions.js';

export function normalizeHome(saved, keys) {
 const order=[...new Set(Array.isArray(saved?.order)?saved.order.filter(key=>keys.includes(key)):[])];
 return {order:[...order,...keys.filter(key=>!order.includes(key))],hidden:Array.isArray(saved?.hidden)?saved.hidden.filter(key=>keys.includes(key)):[],pins:[...new Set(Array.isArray(saved?.pins)?saved.pins.filter(id=>typeof id==='string' && id.length>0 && id.length<200):[])]};
}
// Origins are isolated by browser storage; include the origin explicitly to match native scope.
export function mountMusicHome(panel, labels, renderSection, isCurrent, openPlaylist) {
 const account=state.user, current=()=>isCurrent() && state.user===account;
 let request=0;
 const scope=Number.isSafeInteger(account?.id) && account.id>0?`player.music-home.v1:${location.origin}:${account.id}`:null;
 let saved;try{if(scope)saved=JSON.parse(localStorage.getItem(scope));}catch{/* Corrupt or blocked storage uses readable defaults. */}
 let home=normalizeHome(saved,Object.keys(labels)), playlists=null, failure=false;
 const editor=document.createElement('details'), rows=document.createElement('div');
 editor.innerHTML=html`<summary class="btn">Customize home</summary><div data-home-editor></div>`;
 panel.append(editor,rows);
 const save=(next,focus)=>{
  if(!current())return;
  try{if(scope)localStorage.setItem(scope,JSON.stringify(next));}catch{draw();editor.querySelector(focus)?.focus();toast('Could not remember home. Check browser storage.','error');return;}
  home=next;draw();editor.querySelector(focus)?.focus();
 };
 editor.onkeydown=event=>{
  if(['Enter',' '].includes(event.key) && ['SUMMARY','BUTTON'].includes(event.target.tagName))event.stopPropagation();
  if(event.key==='Escape'){event.stopPropagation();event.preventDefault();editor.open=false;editor.querySelector('summary').focus();}
 };
 function draw(){
  if(!current())return;
  const active=editor.contains(document.activeElement)?document.activeElement:null;
  const focused=active ? [...active.attributes].find(a=>a.name.startsWith('data-home-')) : null;
  const content=editor.querySelector('[data-home-editor]');
  content.innerHTML=html`<p>${scope?'Saved on this device for this server and account.':'Account identity unavailable. Changes last only for this visit.'} Changes apply immediately.</p>`+home.order.map((key,i)=>html`<div class="playlist-actions"><label><input type="checkbox" data-home-show="${key}" ${home.hidden.includes(key)?'':'checked'}> ${esc(labels[key])}</label><button class="btn" data-home-up="${key}" ${i===0?'disabled':''} aria-label="Move ${esc(labels[key])} up">↑ Move up</button><button class="btn" data-home-down="${key}" ${i===home.order.length-1?'disabled':''} aria-label="Move ${esc(labels[key])} down">↓ Move down</button></div>`).join('')+html`<button class="btn" data-home-defaults>Restore defaults</button><h3>Pin playlists</h3>`+(failure?html`<p role="status">Could not load playlists. Check your connection.</p><button class="btn" data-home-retry>Retry playlists</button>`:playlists===null?html`<p role="status">Loading playlists…</p>`:playlists.length?playlists.map(p=>html`<label class="playlist-actions"><input type="checkbox" data-home-pin="${esc(p.playlist_id)}" ${home.pins.includes(p.playlist_id)?'checked':''}> ${esc(p.name)}</label>`).join(''):html`<p>No readable playlists. Create a playlist in Music · Playlists.</p>`);
  content.querySelectorAll('[data-home-show]').forEach(input=>input.onchange=()=>save({...home,hidden:input.checked?home.hidden.filter(key=>key!==input.dataset.homeShow):[...home.hidden,input.dataset.homeShow]},`[data-home-show="${input.dataset.homeShow}"]`));
  for(const direction of ['up','down'])content.querySelectorAll(`[data-home-${direction}]`).forEach(button=>button.onclick=()=>{const key=button.dataset[direction==='up'?'homeUp':'homeDown'],order=[...home.order],i=order.indexOf(key),j=i+(direction==='up'?-1:1);[order[i],order[j]]=[order[j],order[i]];const focusDirection=(j===0||j===order.length-1)?direction==='up'?'down':'up':direction;save({...home,order},`[data-home-${focusDirection}="${key}"]`);});
  content.querySelector('[data-home-defaults]').onclick=()=>save(normalizeHome(null,Object.keys(labels)),'[data-home-defaults]');
  content.querySelector('[data-home-retry]')?.addEventListener('click',loadPlaylists);
  content.querySelectorAll('[data-home-pin]').forEach(input=>input.onchange=()=>save({...home,pins:input.checked?[...home.pins,input.dataset.homePin]:home.pins.filter(id=>id!==input.dataset.homePin)},`[data-home-pin="${CSS.escape(input.dataset.homePin)}"]`));
  rows.replaceChildren();
  for(const key of home.order){if(home.hidden.includes(key))continue;if(key==='pinned')renderPins();else renderSection(rows,key);}
  if(focused)editor.querySelector(`[${focused.name}="${CSS.escape(focused.value)}"]`)?.focus();
  if(home.order.every(key=>home.hidden.includes(key)))rows.innerHTML=html`<p>All home collections are hidden. Use Customize home to show them or restore defaults.</p>`;
 }
 function renderPins(){
  const section=document.createElement('section');section.className='music-heading';section.innerHTML=html`<h3>Pinned playlists</h3>`;
  if(!home.pins.length)section.insertAdjacentHTML('beforeend',html`<p>Choose playlists in Customize home.</p>`);
  else if(playlists===null)section.insertAdjacentHTML('beforeend',html`<p>${failure?'Playlists unavailable. Retry in Customize home.':t('Loading playlists…')}</p>`);
  else {
   const accessible=home.pins.map(id=>playlists.find(p=>p.playlist_id===id)).filter(Boolean);
   for(const playlist of accessible){const item=document.createElement('div');item.innerHTML=html`<button class="btn btn-ghost" data-open>${esc(playlist.name)}</button>${collectionButtons(t('Play playlist'))}<button class="btn" data-unpin>Unpin ${esc(playlist.name)}</button>`;item.querySelector('[data-open]').onclick=()=>openPlaylist(playlist.playlist_id);bindCollectionActions(item,()=>[{kind:'playlist',key:playlist.playlist_id}],current,playlist.name);if(!playlist.song_count)item.querySelectorAll('[data-collection-actions] button').forEach(button=>button.disabled=true);item.querySelector('[data-unpin]').onclick=()=>{save({...home,pins:home.pins.filter(id=>id!==playlist.playlist_id)},'summary');editor.querySelector('summary').focus();};section.append(item);}
   if(accessible.length<home.pins.length){const unavailable=document.createElement('button');unavailable.className='btn';unavailable.textContent=`Remove ${home.pins.length-accessible.length} unavailable pins`;unavailable.onclick=()=>save({...home,pins:accessible.map(p=>p.playlist_id)},'summary');section.append(unavailable);}
  }
  rows.append(section);
 }
 async function loadPlaylists(){const operation=++request;failure=false;playlists=null;draw();try{const result=await API.playlists();if(!current() || operation!==request)return;playlists=result;}catch{if(!current() || operation!==request)return;failure=true;}draw();}
 draw();void loadPlaylists();
}
