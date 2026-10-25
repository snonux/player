import {state} from './state.js';
import {API} from './api.js';
import {musicNetwork} from './network-policy.js';
import {downloadQuality} from './download-quality.js';
import {offlineSavedCollections, offlineSavedSongs, pinMusicCollection, commitAutomaticMusic} from './offline-pins.js';

export const automaticCopyLimit=200;
export function automaticPolicy(value={}) {
 return {enabled:value.enabled===true,playlists:value.playlists===true,starred:value.starred===true,recent:value.recent===true,wifiOnly:value.wifiOnly!==false,hours:[1,6,24].includes(value.hours)?value.hours:6};
}
export function automaticSelected(p){return p.playlists || p.starred || p.recent;}
function key(){return state.user?.id>0?`player-automatic-music-v1:${location.origin}:${state.user.id}`:null;}
export function automaticMusicStatus(){const name=key();if(!name)return null;const saved=JSON.parse(localStorage.getItem(name)||'{}'),active=running?.key===name;const interrupted=saved.unfinishedAttempt===true && !active;return {...saved,...(interrupted?{error:'interrupted',message:'Previous automatic sync was interrupted. Retry to finish keeping music current.'}:{}),policy:automaticPolicy(saved.policy),active};}
export function automaticMusicDue(now=Date.now()){const s=automaticMusicStatus();return Boolean(s?.policy.enabled && automaticSelected(s.policy) && (!s.nextDue || s.nextDue<=now));}
function changed(){document.dispatchEvent(new Event('automatic-music:changed'));}
let running=null;
export function cancelAutomaticMusic(){running?.controller.abort();}
export function saveAutomaticMusicPolicy(value){
 const name=key();if(!name)throw Error('Sign in to configure automatic music.');
 const old=automaticMusicStatus();cancelAutomaticMusic();
 localStorage.setItem(name,JSON.stringify({...old,active:undefined,policy:automaticPolicy(value),nextDue:0,error:null,unfinishedAttempt:false,message:''}));changed();
}

// List endpoints are complete arrays; favorites alone uses offset pagination.
// Resolve every page/detail before changing any playable snapshot.
export async function resolveAutomaticMusic(policy,pins,read,current=()=>true){
 const check=()=>{if(!current())throw new DOMException('Canceled','AbortError');};
 const snapshots=[];
 if(policy.playlists)for(const pin of pins.filter(row=>row.pinned && row.collection.kind==='playlist' && !row.automatic)){
  const detail=await read(`/api/playlists/${encodeURIComponent(pin.collection.key)}`);check();
  if(!Array.isArray(detail.songs))throw Error('Incomplete playlist response.');
  snapshots.push({collection:pin.collection,title:detail.name||pin.title,songs:detail.songs,existing:true,previousURLs:pin.urls});
 }
 if(policy.starred || policy.recent){
  const albums=await read('/api/music/albums');check();if(!Array.isArray(albums))throw Error('Incomplete music catalog.');
  const songs=new Map();
  for(const album of albums){const detail=await read(`/api/music/albums/${encodeURIComponent(album.album_key)}`);check();if(!Array.isArray(detail.songs))throw Error('Incomplete album response.');for(const song of detail.songs)songs.set(song.id,song);}
  if(policy.starred){
   const favorites=new Set();let offset=0;
   for(;;){const page=await read(`/api/media?favorites=true&type=audio&limit=200&offset=${offset}`);check();if(!Array.isArray(page))throw Error('Incomplete favorites response.');for(const song of page)favorites.add(song.id);if(page.length<200)break;offset+=200;if(offset>100000)throw Error('Favorites pagination is too large.');}
   snapshots.push({collection:{kind:'playlist',key:'@automatic-starred'},title:'Automatic music · starred songs',songs:[...songs.values()].filter(song=>favorites.has(song.id))});
  }
  if(policy.recent)snapshots.push({collection:{kind:'playlist',key:'@automatic-recent'},title:'Automatic music · newest 50 songs (recently added)',songs:[...songs.values()].sort((a,b)=>String(b.created_at).localeCompare(String(a.created_at)) || b.id-a.id).slice(0,50)});
 }
 const unique=new Map();for(const snapshot of snapshots)for(const song of snapshot.songs){if(song.type!=='audio' || !Number.isSafeInteger(song.id) || song.id<=0)throw Error('Invalid music response.');unique.set(song.id,song);}
 if(unique.size>automaticCopyLimit)throw Error('More than 200 selected songs. Choose fewer playlists or categories.');
 return {snapshots,songs:[...unique.values()]};
}

export function syncAutomaticMusic({force=false}={}){
 const status=automaticMusicStatus();
 if(!status?.policy.enabled || !automaticSelected(status.policy))return Promise.resolve();
 if(!force && !automaticMusicDue())return status.error?Promise.reject(Object.assign(Error(status.message||'Automatic music remains incomplete.'),{kind:status.error})):Promise.resolve();
 if(running)return running.promise;
 const name=key(),account=state.user,policy=status.policy,policyJSON=JSON.stringify(policy),controller=new AbortController();
 const current=()=>key()===name && state.user===account && !controller.signal.aborted && JSON.stringify(automaticMusicStatus()?.policy)===policyJSON && (!policy.wifiOnly || musicNetwork()==='wifi');
 const store=patch=>{if(key()!==name || state.user!==account || JSON.stringify(automaticMusicStatus()?.policy)!==policyJSON)return;const latest=automaticMusicStatus();localStorage.setItem(name,JSON.stringify({...latest,active:undefined,...patch}));changed();};
 const operation={key:name,controller};running=operation;
 operation.promise=(async()=>{
  store({lastAttempt:Date.now(),nextDue:Date.now()+policy.hours*3600000,error:null,unfinishedAttempt:true});
  try{
   if(policy.wifiOnly && musicNetwork()!=='wifi')throw Object.assign(Error('Automatic music waits for Wi-Fi. Use Music network settings to confirm Wi-Fi manually if detection is unavailable.'),{kind:'policy'});
   const profile=downloadQuality(),pins=await offlineSavedCollections();
   const targets=await resolveAutomaticMusic(policy,pins,path=>API.offlineMusicRead(path,controller.signal),current);
   if(!current())throw new DOMException('Canceled','AbortError');
   if(targets.songs.length)await pinMusicCollection({kind:'playlist',key:'@automatic-work'},'Automatic music',targets.songs,{automatic:true,signal:controller.signal,profile,authorized:current,progress:(done,total)=>store({message:`Saving ${done} of ${total} songs…`})});
   await commitAutomaticMusic(targets.snapshots,{signal:controller.signal,authorized:current});
   if(!current())throw new DOMException('Canceled','AbortError');
   const saved=await offlineSavedSongs();if(!current())throw new DOMException('Canceled','AbortError');
   const desired=new Set(targets.songs.map(song=>song.id));
   store({lastSuccess:Date.now(),members:desired.size,retained:saved.filter(row=>row.automatic).length,retired:saved.filter(row=>row.automatic && !desired.has(row.media.id)).length,message:'Offline music is current.',error:null,unfinishedAttempt:false});
  }catch(error){store({message:error.name==='AbortError'?'Canceled. Completed downloads remain available.':error.message,error:error.name==='AbortError'?'canceled':error.kind|| (error.status===401?'auth':'server'),unfinishedAttempt:false});throw error;}
 })().finally(()=>{if(running===operation)running=null;changed();});
 return operation.promise;
}

export function mountAutomaticMusicSettings(host,syncNow){
 if(!host || host.querySelector('[data-automatic-music]'))return;
 const panel=document.createElement('details');panel.dataset.automaticMusic='';
 panel.innerHTML='<summary>Automatic offline music</summary><p>Keep pinned playlists, starred songs and/or the newest 50 recently added songs current for this account. Completed copies use Download quality. Choose what to keep before enabling.</p><div class="appearance-controls"><label><input type="checkbox" data-enabled> Keep music current</label><label><input type="checkbox" data-playlists> Pinned playlists</label><label><input type="checkbox" data-starred> Starred songs</label><label><input type="checkbox" data-recent> Newest 50 songs (recently added)</label><label>Check every <select data-hours aria-label="Automatic music interval"><option value="1">1 hour</option><option value="6">6 hours</option><option value="24">24 hours</option></select></label><label><input type="checkbox" data-wifiOnly> Download on Wi-Fi only</label></div><p>This browser checks only while this tab is open and visible. Unknown networks wait; use Music network settings → Wi-Fi (manual), including in Firefox. Browser storage may be reclaimed.</p><p role="status" data-status></p><div class="playlist-actions"><button class="btn" data-sync>Sync now</button><button class="btn" data-cancel>Cancel current run</button></div><p>Removed songs leave automatic collections after a complete sync. Retired downloads stay available to protect playback and history. Automatic copies are limited to 200 retained songs. Use Saved music collections → Automatic music · retained downloads → Remove download to free space; other saved collections keep their referenced copies.</p>';
 host.append(panel);
 let error='';
 const render=()=>{try{const s=automaticMusicStatus();for(const field of ['enabled','playlists','starred','recent','wifiOnly']){const input=panel.querySelector(`[data-${field}]`);input.checked=s?.policy[field]??field==='wifiOnly';input.disabled=!s;}panel.querySelector('[data-hours]').value=String(s?.policy.hours||6);panel.querySelector('[data-hours]').disabled=!s;
  const selected=s?['playlists','starred','recent'].filter(field=>s.policy[field]).map(field=>({playlists:'pinned playlists',starred:'starred songs',recent:'newest 50 recently added songs'}[field])).join(', '):'';
  panel.querySelector('[data-status]').textContent=error || (!s?'Sign in to configure automatic music.':`${s.active?'Syncing…':s.policy.enabled?'Enabled':'Paused'} · What will be kept: ${selected||'nothing selected'}. ${s.members||0} current songs · ${s.retained||0} automatic copies · ${s.retired||0} retired. ${s.message||''} Last completed: ${s.lastSuccess?new Date(s.lastSuccess).toLocaleString():'never'}. Last attempt: ${s.lastAttempt?new Date(s.lastAttempt).toLocaleString():'never'}. Next check: ${s.policy.enabled && selected?(s.nextDue?new Date(s.nextDue).toLocaleString():'when this tab is visible'):'paused'}.`);
  panel.querySelector('[data-cancel]').disabled=!s?.active;panel.querySelector('[data-sync]').disabled=!s?.policy.enabled || !automaticSelected(s.policy) || s.active;
 }catch{panel.querySelector('[data-status]').textContent='Automatic music settings unavailable. Allow browser storage and reopen Settings.';}};
 panel.addEventListener('change',()=>{try{const p={};for(const f of ['enabled','playlists','starred','recent','wifiOnly'])p[f]=panel.querySelector(`[data-${f}]`).checked;p.hours=Number(panel.querySelector('[data-hours]').value);saveAutomaticMusicPolicy(p);error='';render();}catch{error='Could not save automatic music settings. Check browser storage.';render();}});
 panel.querySelector('[data-sync]').onclick=syncNow;panel.querySelector('[data-cancel]').onclick=cancelAutomaticMusic;
 document.addEventListener('automatic-music:changed',render);document.addEventListener('podcast:account-changed',()=>{cancelAutomaticMusic();render();});
 document.addEventListener('music-network:changed',()=>{if(running)running.controller.abort();render();});
 window.addEventListener('storage',e=>{if(e.key?.startsWith('player-automatic-music-v1:')){cancelAutomaticMusic();render();}});
 render();
}
