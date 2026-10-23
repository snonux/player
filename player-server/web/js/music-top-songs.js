import { escapeHtml as esc } from './utils.js';
import { mediaTitle } from './music.js';
import { insertQueue, queueState } from './queue.js';

export function topSongsSource(source) {
 return source === 'personal' ? 'From your library · your most-played credited songs' : source === 'global' ? 'From your library · server-wide plays (no personal plays yet)' : 'Local ranking · no external popularity provider';
}

// Buttons keep the artist's discography selection and vi navigation intact.
export function mountArtistTopSongs(host, ranking, artist, current) {
 const songs=ranking?.songs || [], source=ranking?.source || 'none';
 const section=document.createElement('section');
 section.className='grid-full'; section.setAttribute('aria-label','Top songs');
 section.innerHTML=`<h2>Top songs</h2><p class="text-muted">${esc(topSongsSource(source))}</p>`+(songs.length ? `<button class="btn" data-top-play="0">▶ Play top songs</button><ol>${songs.map((song,index)=>`<li class="card-body"><button class="btn btn-ghost queue-track" data-top-play="${index}" aria-label="Play ${esc(mediaTitle(song))}">${esc(mediaTitle(song))}</button><span class="text-muted text-sm">${esc(song.music?.album || '')} · ${source==='personal'?song.personal_play_count:song.play_count} ${source==='personal'?'personal':'server-wide'} plays</span></li>`).join('')}</ol><p role="status" data-top-status></p>` : '<p>No top songs yet. Play this artist to build a local ranking. You can still use Play artist.</p>');
 host.append(section);
 const alive=()=>section.isConnected && current();
 section.querySelectorAll('[data-top-play]').forEach(button=>button.onclick=async()=>{
  if(!alive())return;
  const status=section.querySelector('[data-top-status]');
  if(queueState().loading){status.textContent='Loading track… Please wait.';return;}
  status.textContent='Loading top songs…';
  try {
   const started=await insertQueue(songs.slice(Number(button.dataset.topPlay)),'now',{replace:true,source:`${artist} · Top songs`});
   if(alive())status.textContent=started?'':'Playback was not started. Try again.';
  } catch(error) {if(alive())status.textContent=`Could not play top songs. ${error.message || 'Check your connection.'}`;}
 });
 return section;
}
