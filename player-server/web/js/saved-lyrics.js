import { state } from './state.js';

// Bodies stay lazy in the audio's account cache; manifests hold only key/size.
export async function snapshotLyrics(id, audioURL, etag, signal, cache) {
 const originalSignal=signal;
 const controller=new AbortController(),timer=setTimeout(()=>controller.abort(),8000);
 const abort=()=>controller.abort();signal?.addEventListener('abort',abort,{once:true});
 if(signal?.aborted)controller.abort();
 signal=controller.signal;
 try {
  if (!etag || signal?.aborted) return null;
  const response = await fetch(`/api/media/${id}/lyrics`, {credentials:'same-origin',redirect:'manual',cache:'no-store',signal});
  if (!response.ok || Number(response.headers.get('content-length')) > 1048576) return null;
  const reader=response.body.getReader(); const chunks=[]; let size=0;
  try { while(true) { const {done,value}=await reader.read(); if(done)break; size+=value.length; if(size>1048576){await reader.cancel();return null;} chunks.push(value); } }
  finally {reader.releaseLock();}
  const data=JSON.parse(await new Blob(chunks).text());
  if (!Array.isArray(data?.lines)) return null;
  const check=await fetch(audioURL,{method:'HEAD',credentials:'same-origin',redirect:'manual',cache:'no-store',signal});
  if(!check.ok || check.headers.get('etag')!==etag || !data.lines.length)return null;
  const key=new URL(audioURL,location.origin);key.searchParams.set('player_lyrics','1');
  const body=JSON.stringify(data),lyricSize=new TextEncoder().encode(body).length;
  await cache.put(key.href,new Response(body,{headers:{'content-type':'application/json','x-player-audio-etag':etag}}));
  return {lyricsKey:key.href,lyricsSize:lyricSize};
 } catch { return null; } // Metadata failure never invalidates audio.
 finally {clearTimeout(timer);originalSignal?.removeEventListener('abort',abort);}
}

export async function savedLyrics(media, streamURL, signal, backing) {
 if(!Number.isSafeInteger(media?.id)||media.id<=0)return null;
 const id=state.user?.id;
 if(!id || !globalThis.caches || !backing?.etag || ![`player-offline-v1-${id}`,`player-playback-v1-${id}`].includes(backing.name))return null;
 let exact;try{exact=new URL(streamURL,location.origin).href;if(exact!==backing.url || new URL(exact).origin!==location.origin || media.offlinePinURL && new URL(media.offlinePinURL,location.origin).href!==exact)return null;}catch{return null;}
 {
  const name=backing.name;
  let rows;
  try {const index=JSON.parse(localStorage.getItem(`${name}:index`)||'{}'); rows=Object.values(index.songs||index);}catch{return null;}
  for(const row of rows) {
   if(row?.media?.id!==media.id || row.url!==exact || row.etag!==backing.etag || typeof row.lyricsKey!=='string')continue;
   const cache=await caches.open(name);
   if(await cache.match(row.url)) {
    let lyrics;try{const response=await cache.match(row.lyricsKey);if(!response || response.headers.get('x-player-audio-etag')!==backing.etag)continue;lyrics=await response.json();if(!Array.isArray(lyrics?.lines))continue;}catch{continue;}
    if(signal?.aborted || state.user?.id!==id)return null;
    return {...lyrics,provider_enabled:false,saved:true};
   }
  }
 }
 return null;
}
