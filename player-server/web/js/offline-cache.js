import { offlineMusicAccount } from './offline-context.js';
const prefix='player-playback-v1-';
const current=(id,signal)=>{if(offlineMusicAccount()!==id || signal?.aborted)throw new DOMException('Canceled','AbortError');};
function rows(id) {
 try {
  const data=JSON.parse(localStorage.getItem(`${prefix}${id}:index`) || '{}');
  return Object.values(data).filter(row=>row?.media?.type==='audio' && Number.isSafeInteger(row.media.id) && row.media.id>0 && typeof row.url==='string' && row.size>0);
 } catch {return [];}
}
// Read-only view over the automatic playback cache's existing manifest/bytes.
export function offlineCachedAccounts() {
 const ids=[];
 for(let i=0;i<localStorage.length;i++) {
  const match=localStorage.key(i)?.match(/^player-playback-v1-([1-9]\d*):index$/);
  if(match && Number.isSafeInteger(Number(match[1])))ids.push(Number(match[1]));
 }
 return ids;
}
export async function offlineCachedSongs() {
 const id=offlineMusicAccount();if(!id || !globalThis.caches)return [];
 const cache=await caches.open(`${prefix}${id}`),saved=[];
 for(const row of rows(id)) if(await cache.match(row.url))saved.push({...row,storageKind:'cache'});
 current(id);return saved;
}
export async function offlineCachedResponse(url,signal) {
 const id=offlineMusicAccount();if(!id || !globalThis.caches)return null;
 if(!rows(id).some(row=>row.url===url))return null;
 const response=await (await caches.open(`${prefix}${id}`)).match(url);
 current(id,signal);return response || null;
}
