import { mediaTitle } from './music.js';
import { waitForCompatStream } from './streamSource.js';

// A guest owns a separate player and never imports the authenticated queue.
export function showShareCollection(data) {
 document.getElementById('player').classList.add('hidden');
 const root=document.createElement('section');root.setAttribute('aria-label','Shared collection');
 const audio=document.createElement('audio');audio.controls=true;audio.preload='none';audio.setAttribute('aria-label','Shared music player');
 const status=document.createElement('p');status.setAttribute('role','status');
 const controls=document.createElement('div');controls.className='playlist-actions';
 const previous=document.createElement('button'),next=document.createElement('button');previous.textContent='Previous';next.textContent='Next';previous.className=next.className='btn';controls.append(previous,next);
 const list=document.createElement('ol');list.className='share-track-list';
 root.append(audio,controls,status,list);document.getElementById('player').after(root);
 let index=-1,pending=null,generation=0;
 const available=i=>i>=0&&i<data.items.length&&!data.items[i].unavailable;
 const adjacent=(from,step)=>{for(let i=from+step;i>=0&&i<data.items.length;i+=step)if(available(i))return i;return -1;};
 const rows=data.items.map((item,i)=>{
  const row=document.createElement('li'),play=document.createElement('button');play.className='btn btn-ghost';play.textContent=mediaTitle(item.media);play.disabled=!!item.unavailable;play.onclick=()=>void select(i,true);row.append(play);
  if(item.download_url){const download=document.createElement('a');download.href=item.download_url;download.textContent='Download';download.setAttribute('aria-label',`Download ${mediaTitle(item.media)}`);row.append(download);}
  list.append(row);return play;
 });
 async function select(i,play){
  if(!available(i))return;
  const operation=++generation;pending?.abort();pending=new AbortController();audio.pause();audio.removeAttribute('src');audio.load();index=i;
  rows.forEach((row,n)=>row.setAttribute('aria-current',n===i?'true':'false'));
  previous.disabled=adjacent(i,-1)<0;next.disabled=adjacent(i,1)<0;
  const item=data.items[i];status.textContent=`${i+1} of ${data.items.length}: ${mediaTitle(item.media)}`;
  try{
   if(item.transcoded){status.textContent='Preparing audio…';const result=await waitForCompatStream(item.playback_url,{signal:pending.signal});if(operation!==generation)return;if(result.state!=='ready')throw Error('Could not prepare audio. Select this song to retry.');}
   if(operation!==generation)return;
   audio.src=item.playback_url||item.stream_url;audio.load();status.textContent=`${i+1} of ${data.items.length}: ${mediaTitle(item.media)}`;
   if(play)await audio.play();
  }catch{if(operation===generation)status.textContent='Could not play this song. Check your connection or whether the link expired, then select a song to retry.';}
 }
 previous.onclick=()=>void select(adjacent(index,-1),true);next.onclick=()=>void select(adjacent(index,1),true);
 audio.addEventListener('ended',()=>{const i=adjacent(index,1);if(i>=0)void select(i,true);else status.textContent='End of collection.';});
 audio.addEventListener('error',()=>{status.textContent='This song is unavailable. The link may have expired or been revoked. Select a song to retry.';});
 window.addEventListener('pagehide',()=>{pending?.abort();audio.pause();},{once:true});
 document.addEventListener('keydown',event=>{
  // Browser chords, composition, held keys and native audio controls keep
  // their own behavior. Letter shortcuts still work after a track button or
  // download link keeps focus; Space outside dialogs stays play/pause.
  if(event.defaultPrevented || event.isComposing || event.keyCode===229 || event.repeat || event.ctrlKey || event.metaKey || event.altKey || event.shiftKey)return;
  const target=event.target;
  if(/^(INPUT|SELECT|TEXTAREA|AUDIO)$/.test(target.tagName) || target.isContentEditable || target.closest?.('.modal-overlay, [role="dialog"], dialog'))return;
  if(event.key!=='p' && event.key!==' ' && event.code!=='Space')return;
  event.preventDefault();
  if(audio.paused)void audio.play().catch(()=>{status.textContent='Press Play to try again.';});else audio.pause();
 });
 const first=data.items.findIndex(item=>!item.unavailable);if(first>=0)void select(first,false);else{previous.disabled=next.disabled=true;status.textContent='No songs are currently available in this collection.';}
}
