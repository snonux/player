import { state } from './state.js';
import { escapeHtml as esc, toast } from './utils.js';
import { redirectToLogin } from './api.js';
import { currentMediaInfo, playLiveRadio, pausePlayback, stopAndClose } from './playback.js';

let stations = [], selected = 0, revision = 0, streamState = 'stopped';
const byId = id => document.getElementById(id);
const visible = () => stations.filter(s => s.name.toLocaleLowerCase().includes(byId('radio-search').value.toLocaleLowerCase()));
async function radioAPI(path = '', options = {}) {
 let response;
 try { response = await fetch(`/api/radio${path}`, {credentials:'same-origin', ...options, headers:{'Content-Type':'application/json'},body:options.body ? JSON.stringify(options.body) : undefined}); }
 catch { throw new Error('Could not reach stations. Check your connection and retry.'); }
 if (response.status === 401) redirectToLogin();
 if (!response.ok) { let message = 'Could not reach stations. Check your connection and retry.'; try {message = (await response.json()).error || message;} catch {} throw new Error(message); }
 return response.status === 204 ? null : response.json();
}
export function initRadio() {
 byId('internet-radio')?.addEventListener('click', () => {byId('radio-modal').classList.add('open'); load();});
 byId('radio-close')?.addEventListener('click', () => byId('radio-modal').classList.remove('open'));
 byId('radio-refresh')?.addEventListener('click',load);
 byId('radio-add')?.addEventListener('click',()=>edit());
 byId('radio-search')?.addEventListener('input',()=>{selected=0;render();});
 byId('radio-editor-close')?.addEventListener('click',()=>byId('radio-editor').classList.remove('open'));
 byId('radio-form')?.addEventListener('submit',save);
 byId('radio-modal')?.addEventListener('keydown',keys);
 byId('radio-editor')?.addEventListener('keydown',event=>{if(event.key==='Escape'){event.preventDefault();event.stopPropagation();byId('radio-editor').classList.remove('open');}});
 for (const event of ['playing','waiting','stalled','pause','error','ended','emptied','loadstart']) byId('media-audio')?.addEventListener(event, updateStatus);
 window.addEventListener('offline',updateStatus);
 window.addEventListener('online',updateStatus);
}
async function load() {
 const operation=++revision;
 byId('radio-status').textContent='Loading stations…';
 byId('radio-add').hidden=!state.isAdmin;
 try {const result=await radioAPI();if(operation!==revision)return;stations=result;selected=Math.min(selected,Math.max(0,visible().length-1));render();}
 catch(error){if(operation===revision)byId('radio-status').textContent=error.message;}
}
function render() {
 const rows=visible();
 byId('radio-status').textContent=rows.length ? 'Live stations · j/k or arrows to select, Enter or p to play, / to search.' : stations.length ? 'No matching stations.' : 'No stations yet. Ask an administrator to add one.';
 byId('radio-list').innerHTML=rows.map((station,index)=>`<article class="radio-station ${index===selected?'selected':''}" data-station="${station.id}"><img class="radio-art" alt="" src="${station.artwork_url?`/api/radio/${station.id}/artwork`:''}" ${station.artwork_url?'':'hidden'}><div class="radio-description"><h4>${esc(station.name)}</h4><p class="radio-state" role="status">Live radio</p><div class="radio-actions"><button class="btn radio-play" data-play="${station.id}">Play</button>${station.homepage_url?`<a class="btn btn-ghost" href="${esc(station.homepage_url)}" target="_blank" rel="noopener noreferrer" referrerpolicy="no-referrer">Station website</a>`:''}${state.isAdmin?`<button class="btn btn-ghost" data-edit="${station.id}">Edit</button><button class="btn btn-danger" data-remove="${station.id}">Remove</button>`:''}</div></div></article>`).join('');
 byId('radio-list').querySelectorAll('img').forEach(img=>img.addEventListener('error',()=>{img.hidden=true;},{once:true}));
 byId('radio-list').querySelectorAll('[data-play]').forEach(button=>button.addEventListener('click',()=>play(find(button.dataset.play))));
 byId('radio-list').querySelectorAll('[data-edit]').forEach(button=>button.addEventListener('click',()=>edit(find(button.dataset.edit))));
 byId('radio-list').querySelectorAll('[data-remove]').forEach(button=>button.addEventListener('click',()=>remove(find(button.dataset.remove))));
 byId('radio-list').querySelectorAll('article').forEach((row,index)=>row.addEventListener('focusin',()=>select(index,false)));
 updateStatus();
}
const find = id => stations.find(station=>station.id===Number(id));
async function play(station) {
 if (!station) return;
 const audio=byId('media-audio');
 if (currentMediaInfo()?.radio_station_id===station.id && !audio.paused) {pausePlayback();return;}
 // Main playback owns decoder, system controls, and renderer handoff.
 const media={type:'audio',file_name:station.name,live_radio:true,radio_station_id:station.id,music:{title:station.name,artist:'Live radio',album:'Internet radio'}};
 try {await playLiveRadio(media,`/api/radio/${station.id}/stream`,station.artwork_url?`/api/radio/${station.id}/artwork`:'');updateStatus();}
 catch(error){byId('radio-status').textContent=error.message;}
}
function updateStatus() {
 const current=currentMediaInfo(), audio=byId('media-audio');
 if (audio && !audio.getAttribute('src')) streamState='stopped';
 else if (arguments[0]?.type) {
  const type=arguments[0].type;
  if (['waiting','stalled','loadstart'].includes(type)) streamState='buffering';
  else if(type==='playing')streamState='live';
  else if(['error','ended'].includes(type))streamState='failed';
  else if(['pause','emptied'].includes(type))streamState='stopped';
 }
 byId('radio-list')?.querySelectorAll('[data-station]').forEach(row=>{
  const active=current?.radio_station_id===Number(row.dataset.station);
  const failed=active && (audio.error || audio.ended || streamState==='failed');
  const buffering=active && (streamState==='buffering' || !audio.paused && audio.readyState<3);
  row.querySelector('.radio-state').textContent=!active?'Live radio':!navigator.onLine?'Offline · reconnect, then Retry':failed?'Station disconnected · Retry':buffering?'Buffering…':audio.paused?'Stopped · Play reconnects live':'Live';
  row.querySelector('.radio-play').textContent=active && (!navigator.onLine || failed)?'Retry':active && !audio.paused?'Stop':'Play';
 });
}
function select(index,focus=true) {
 const rows=[...byId('radio-list').children];selected=Math.max(0,Math.min(index,rows.length-1));
 rows.forEach((row,i)=>row.classList.toggle('selected',i===selected));
 if(focus) rows[selected]?.querySelector('button')?.focus();
}
function keys(event) {
 if(event.target.matches('input,textarea,select') || event.key===' ' && event.target.closest('button,a'))return;
 if(['j','ArrowDown','l','ArrowRight','k','ArrowUp','h','ArrowLeft'].includes(event.key)){event.preventDefault();event.stopPropagation();select(selected+(['j','ArrowDown','l','ArrowRight'].includes(event.key)?1:-1));}
 else if(event.key==='/'){event.preventDefault();event.stopPropagation();byId('radio-search').focus();}
 else if(event.key==='p'||event.key===' '||event.key==='Enter' && !event.target.closest('button,a')){event.preventDefault();event.stopPropagation();play(visible()[selected]);}
 else if(event.key==='Escape'){event.preventDefault();event.stopPropagation();byId('radio-modal').classList.remove('open');}
}
function edit(station=null) {
 const form=byId('radio-form');form.dataset.station=station?.id || '';
 for(const key of ['name','stream_url','homepage_url','artwork_url'])form.elements[key].value=station?.[key] || '';
 form.elements.local_network.checked=!!station?.local_network;
 byId('radio-editor-title').textContent=station?'Edit station':'Add station';
 byId('radio-editor-status').textContent='';byId('radio-editor').classList.add('open');
}
async function save(event) {
 event.preventDefault();const form=event.currentTarget,button=form.querySelector('[type=submit]');
 const body=Object.fromEntries(['name','stream_url','homepage_url','artwork_url'].map(key=>[key,form.elements[key].value.trim()]));body.local_network=form.elements.local_network.checked;
 button.disabled=true;
 try {await radioAPI(form.dataset.station?`/${form.dataset.station}`:'',{method:form.dataset.station?'PUT':'POST',body});byId('radio-editor').classList.remove('open');await load();select(selected);}
 catch(error){byId('radio-editor-status').textContent=error.message;}
 finally{button.disabled=false;}
}
async function remove(station) {
 if(!confirm(`Remove ${station.name} for everyone?`))return;
 try {await radioAPI(`/${station.id}`,{method:'DELETE'});if(currentMediaInfo()?.radio_station_id===station.id)stopAndClose();await load();select(selected);}
 catch(error){toast(error.message,'error');}
}
