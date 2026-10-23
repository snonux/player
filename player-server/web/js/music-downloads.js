import { state } from './state.js';

export function uniqueDownloadSongs(songs) {
 const seen = new Set();
 return songs.filter(song => song.type === 'audio' && Number.isSafeInteger(song.id) && song.id > 0 && !seen.has(song.id) && seen.add(song.id));
}

// A single ZIP avoids browser automatic-multiple-download permission prompts.
// Originals remain separate from the automatic playback cache.
export function downloadMusic(songs, opener = document.activeElement) {
 songs = uniqueDownloadSongs(songs);
 const user = state.user;
 const overlay = document.createElement('div'); overlay.className = 'modal-overlay open';
 overlay.innerHTML = `<section class="modal" role="dialog" aria-modal="true" aria-labelledby="music-download-title"><div class="modal-header"><h2 id="music-download-title">Download music</h2></div><div class="modal-body"><p>${songs.length} unique songs · Original files in one ZIP</p><p>Extract the ZIP to play files offline with a compatible player. Downloads do not add songs to this browser’s offline library.</p><p>Up to 500 songs and 512 MiB per archive. For larger collections, select smaller groups of songs. Keep this tab open while preparing and downloading.</p><p role="status" aria-live="polite" data-status></p><progress hidden aria-label="Archive download"></progress><div class="playlist-actions"><button class="btn" data-close>Close</button><button class="btn btn-primary" data-start>Download ZIP</button></div></div></section>`;
 const status=overlay.querySelector('[data-status]'), start=overlay.querySelector('[data-start]'), close=overlay.querySelector('[data-close]'), progress=overlay.querySelector('progress');
 let controller=null, closed=false;
 const finish=()=>{closed=true;controller?.abort();overlay.remove();if(opener?.isConnected)opener.focus();};
 close.onclick=finish;
 overlay.onkeydown=event=>{
  if(event.key==='Escape'){event.preventDefault();event.stopPropagation();finish();}

 };
 start.disabled=!songs.length || songs.length>500;
 if(start.disabled)status.textContent=songs.length?'Select at most 500 songs per download.':'No songs available.';
 start.onclick=async()=>{
  controller=new AbortController();start.disabled=true;close.textContent='Cancel';progress.hidden=true;
  const current=()=>!closed && state.user===user;
  const accountChanged=()=>{if(!current())controller.abort();};
  // Credential state can change without navigating; never save a former account's result.
  const timer=setInterval(accountChanged,200);
  status.textContent='Preparing archive… Checking access and reading original files. No files are saved if preparation fails.';
  try{
   const response=await fetch('/api/music/downloads',{method:'POST',credentials:'include',headers:{'Content-Type':'application/json'},body:JSON.stringify({media_ids:songs.map(song=>song.id)}),signal:controller.signal});
   if(!response.ok){let message='Could not prepare archive. Check your connection and access, then retry.';try{message=(await response.json()).error || message;}catch{}throw Error(message);}
   if(!response.headers.get('Content-Type')?.includes('application/zip'))throw Error('Server did not return a music archive.');
   const total=Number(response.headers.get('Content-Length'));
   if(total>512*1024*1024)throw Error('Archive exceeds 512 MiB. Select fewer songs.');
   const reader=response.body.getReader(),chunks=[];let received=0;progress.hidden=false;
   if(total>0)progress.max=total;else progress.removeAttribute('value');
   while(true){const {done,value}=await reader.read();if(done)break;received+=value.byteLength;if(received>512*1024*1024){controller.abort();throw Error('Archive exceeds 512 MiB. Select fewer songs.');}chunks.push(value);if(total>0)progress.value=received;status.textContent=`Downloading archive… ${(received/1048576).toFixed(1)} MiB${total>0?` of ${(total/1048576).toFixed(1)} MiB`:''}`;}
   if(!current())throw Error('Account changed. Download canceled.');
   if(!received || (total>0 && received!==total))throw Error('Download interrupted. Retry to save a complete archive.');
   const url=URL.createObjectURL(new Blob(chunks,{type:'application/zip'}));
   const link=document.createElement('a');link.href=url;link.download='Player-music.zip';document.body.append(link);link.click();link.remove();setTimeout(()=>URL.revokeObjectURL(url),60000);
   status.textContent=`Archive ready: ${response.headers.get('X-Download-Count') || songs.length} songs. Check your browser’s downloads to finish saving.`;start.textContent='Download again';
  }catch(error){if(!closed)status.textContent=controller.signal.aborted?'Download canceled. Retry to download the complete archive.':error.message;start.textContent='Retry';}
  finally{clearInterval(timer);if(!closed){start.disabled=!current();close.textContent='Close';progress.hidden=true;controller=null;}}
 };
 document.body.append(overlay);close.focus();
}
