import { offlineCachedSongs, offlineCachedResponse } from './offline-cache.js';
import { offlineMusicMode, offlineMusicAccount } from './offline-context.js';
import { offlinePinnedDetail, offlinePinnedResponse } from './offline-pins.js';
import { initNetworkSettings, assertMusicStreamingAllowed, musicPrefetchAllowed, MusicNetworkRestricted } from './network-policy.js';
import { mediaPlaybackUrl } from './streamSource.js';
import { snapshotLyrics } from './saved-lyrics.js';

const prefix = 'player-playback-v1-';
const preferencesKey = 'player-playback-cache';
let account = null, upcomingSong = null, cacheStatus = '';
export function setPlaybackCacheAccount(id) { releasePlaybackCache(); cacheStatus = ''; account = Number.isSafeInteger(id) && id > 0 ? id : null; changed(); }
let transfer = null, serial = Promise.resolve(), liveURL = null;
const defaults = { budget: 128, prefetch: false, buffer: 'stream' };
export function cachePreferences() {
  try { const p = JSON.parse(localStorage.getItem(preferencesKey)); return { budget: [0,128,512,1024].includes(p?.budget) ? p.budget : 128, prefetch: p?.prefetch === true, buffer: p?.buffer === 'complete' ? 'complete' : 'stream' }; }
  catch { return { ...defaults }; }
}
function scope() { return account ? `${prefix}${account}` : null; }
function indexKey(name) { return `${name}:index`; }
function index(name) {
  try {
    const rows=JSON.parse(localStorage.getItem(indexKey(name)));
    if (!rows || typeof rows !== 'object' || Array.isArray(rows)) return {};
    return Object.fromEntries(Object.entries(rows).filter(([url,r]) => r && r.url===url && r.media?.type==='audio' && Number.isSafeInteger(r.media.id) && r.media.id>0 && Number.isSafeInteger(r.size) && r.size>0 && Number.isFinite(r.used)));
  } catch { return {}; }
}
function saveIndex(name, rows) { localStorage.setItem(indexKey(name), JSON.stringify(rows)); }
function changed() { document.dispatchEvent(new Event('playback-cache:changed')); }
function exclusive(action) {
  const run = () => navigator.locks ? navigator.locks.request('player-playback-cache', action) : action();
  const result = serial.then(run, run); serial = result.catch(() => {}); return result;
}
export function cancelCacheTransfer() { transfer?.controller.abort(); }
export function releasePlaybackCache() { cancelCacheTransfer(); if (liveURL) URL.revokeObjectURL(liveURL); liveURL = null; }
function valid(name, signal) { return scope() === name && !signal?.aborted; }
export async function cachedMediaDetail(id, load) {
  if (offlineMusicMode()) {
    const detail = await offlinePinnedDetail(id) || (await offlineCachedSongs()).filter(row => String(row.media.id) === String(id)).map(row => ({media:{...row.media,offlinePinURL:row.url},progress:null}))[0];
    if (!detail) throw new TypeError('Saved download unavailable.');
    return detail;
  }
  try { return await load(); }
  catch (error) {
    if (!(error instanceof TypeError) || !scope() || !globalThis.caches) throw error;
    const pinned = await offlinePinnedDetail(id); if (pinned) return pinned;
    const name = scope(), rows = index(name);
    const row = Object.values(rows).find(r => String(r.media.id) === String(id));
    if (!row || !(await (await caches.open(name)).match(row.url))) throw error;
    return { media: row.media, progress: null };
  }
}
async function prune(name, budget) {
  const cache = await caches.open(name), rows = index(name);
  let size = Object.values(rows).reduce((sum, r) => sum + r.size + (r.lyricsSize || 0), 0);
  for (const row of Object.values(rows).sort((a,b) => a.used-b.used)) {
    if (size <= budget) break;
    await cache.delete(row.url); if(row.lyricsKey)await cache.delete(row.lyricsKey); delete rows[row.url]; size -= row.size + (row.lyricsSize || 0);
  }
  saveIndex(name, rows);
}
async function fill(media, url, name, signal) {
  return exclusive(async () => {
    if (!valid(name, signal)) throw new DOMException('Cancelled', 'AbortError');
    const budget = cachePreferences().budget * 1024 * 1024;
    if (!budget) return;
    const cache = await caches.open(name);
    if (await cache.match(url)) return;
    const response = await fetch(url, { credentials: 'same-origin', redirect: 'manual', cache: 'no-store', signal });
    if (response.status !== 200 || !response.body || !(response.headers.get('content-type') || '').startsWith('audio/')) throw Error(`Cache request failed (${response.status})`);
    const expected = Number(response.headers.get('content-length'));
    if (expected > budget) throw Error('Song is larger than the cache budget.');
    let size = 0;
    const bounded = response.body.pipeThrough(new TransformStream({ transform(chunk, controller) {
      if (!valid(name, signal)) throw new DOMException('Cancelled', 'AbortError');
      size += chunk.byteLength;
      if (size > budget) throw Error('Song is larger than the cache budget.');
      controller.enqueue(chunk);
    }}));
    await cache.put(url, new Response(bounded, { headers: response.headers }));
    if (!valid(name, signal) || !size || (expected > 0 && size !== expected)) { await cache.delete(url); throw Error('Incomplete cache copy.'); }
    const rows = index(name); rows[url] = { url, media, size, used: Date.now(), etag: response.headers.get('etag') || '' };
    Object.assign(rows[url],await snapshotLyrics(media.id,url,rows[url].etag,signal,cache));
    if(!valid(name,signal)){await cache.delete(url);if(rows[url].lyricsKey)await cache.delete(rows[url].lyricsKey);throw new DOMException('Cancelled','AbortError');}
    try { saveIndex(name, rows); await prune(name, budget); }
    catch(error) { await cache.delete(url); if(rows[url]?.lyricsKey)await cache.delete(rows[url].lyricsKey); const restored=index(name); delete restored[url]; try { saveIndex(name,restored); } catch {} throw error; }
    changed();
  });
}
export async function cacheSong(media, url = mediaPlaybackUrl(media), { prefetch = false } = {}) {
  if (media?.offlinePinURL) return;
  assertMusicStreamingAllowed(media);
  if (prefetch && !musicPrefetchAllowed()) return;
  const name = scope();
  if (!name || !globalThis.caches || media?.type !== 'audio' || !cachePreferences().budget) return;
  if (transfer) { if (prefetch) return; cancelCacheTransfer(); }
  const job = { controller: new AbortController(), title: media.music?.title || media.file_name || 'Song' };
  cacheStatus = ''; transfer = job; changed();
  try { await fill(media, new URL(url, location.origin).href, name, job.controller.signal); }
  catch(error) {
    if (!job.controller.signal.aborted && scope() === name) cacheStatus = 'Could not cache this song. Check browser storage and connection. Streaming remains available.';
    throw error;
  }
  finally { if (transfer === job) { transfer = null; changed(); if (!prefetch && !job.controller.signal.aborted && cachePreferences().prefetch && musicPrefetchAllowed()) { const next=upcomingSong?.(); if(next) void cacheSong(next,undefined,{prefetch:true}).catch(()=>{}); } } }
}
// HEAD always revalidates access and rendition identity online. Only a transport
// failure may reuse a copy; an HTTP denial never becomes offline permission.
export function playbackCacheEnabled(media) { return media?.type === "audio" && (!!media.offlinePinURL || (!!scope() && !!globalThis.caches)); }
export async function playbackCacheSource(media, url, signal, onSaved = () => {}) {
  // Explicit downloaded playback has its own local policy. It must never turn
  // into an audio network request if the bytes disappear or access is refused.
  if (media?.offlinePinURL) {
    const pinScope = offlineMusicMode() ? offlineMusicAccount() : scope();
    if (new URL(media.offlinePinURL, location.origin).href !== new URL(url, location.origin).href ||
        mediaPlaybackUrl(media) !== new URL(url, location.origin).href) throw Error('Invalid saved download source.');
    let pinned = await offlinePinnedResponse(url, signal);
    const fromPin=!!pinned;
    pinned ||= offlineMusicMode() ? await offlineCachedResponse(url,signal) : null;
    if (!pinned) throw Error('Saved download unavailable. Retry pinning or choose online playback.');
    const blob = await pinned.blob();
    if (signal?.aborted || (offlineMusicMode() ? offlineMusicAccount() : scope()) !== pinScope) throw new DOMException('Cancelled','AbortError');
    onSaved({name:`player-${fromPin?'offline':'playback'}-v1-${offlineMusicMode()?offlineMusicAccount():account}`,url:new URL(url,location.origin).href,etag:pinned.headers.get('etag') || ''});
    liveURL = URL.createObjectURL(blob); return liveURL;
  }
  assertMusicStreamingAllowed(media);
  const pinScope = scope();
  try {
    const pinned = await offlinePinnedResponse(url,signal);
    if (pinned) {
      const blob = await pinned.blob();
      if (signal?.aborted || scope() !== pinScope) throw new DOMException('Cancelled','AbortError');
      assertMusicStreamingAllowed(media);
      onSaved({name:`player-offline-v1-${account}`,url:new URL(url,location.origin).href,etag:pinned.headers.get('etag') || ''});
      liveURL = URL.createObjectURL(blob); return liveURL;
    }
    const source = await resolveCacheSource(media,url,signal,onSaved);
    assertMusicStreamingAllowed(media); return source;
  }
  catch(error) { if (signal?.aborted || error instanceof MusicNetworkRestricted) throw error; assertMusicStreamingAllowed(media); cacheStatus = 'Playback cache is unavailable. Streaming remains available; check browser storage.'; changed(); return url; }
}
async function resolveCacheSource(media, url, signal, onSaved) {
  const name = scope();
  if (!name || !globalThis.caches || media.type !== 'audio' || !cachePreferences().budget) return url;
  url = new URL(url, location.origin).href;
  const cache = await caches.open(name), row = index(name)[url];
  let response = row ? await cache.match(url) : null;
  if (response) {
    let check;
    try { check = await fetch(url, { method: 'HEAD', credentials: 'same-origin', cache: 'no-store', redirect: 'manual', signal }); }
    catch (error) { if (!(error instanceof TypeError) || signal?.aborted || scope() !== name) throw error; }
    if (check && !check.ok) {
      if ([401,403,404,410].includes(check.status)) {
        try { await cache.delete(url); const rows=index(name); if(rows[url]?.lyricsKey)await cache.delete(rows[url].lyricsKey); delete rows[url]; saveIndex(name,rows); } catch { /* Denial still returns the network URL. */ }
      }
      return url;
    }
    if (check && row.etag && check.headers.get('etag') !== row.etag) { await cache.delete(url); const rows=index(name); if(rows[url]?.lyricsKey)await cache.delete(rows[url].lyricsKey); delete rows[url]; saveIndex(name,rows); response = null; }
  }
  if (!response && cachePreferences().buffer === 'complete') {
    try { await cacheSong(media, url); response = await cache.match(url); } catch { /* Cache failure keeps ordinary streaming available. */ }
  }
  if (signal?.aborted || scope() !== name) throw new DOMException('Cancelled', 'AbortError');
  if (response) {
    const rows=index(name); if (rows[url]) { rows[url].used=Date.now(); saveIndex(name,rows); }
    const blob = await response.blob();
    if (signal?.aborted || scope() !== name) throw new DOMException('Cancelled', 'AbortError');
    onSaved({name,url,etag:response.headers.get('etag') || ''});
    liveURL = URL.createObjectURL(blob); return liveURL;
  }
  return url;
}
export function initPlaybackCache(nextSong) {
  upcomingSong = nextSong;
  const host = document.getElementById('settings-modal')?.querySelector('.modal');
  if (!host) return;
  initNetworkSettings(host);
  document.addEventListener('music-network:changed',cancelCacheTransfer);
  const panel = document.createElement('section'); panel.innerHTML = '<h4>Playback cache</h4><p class="text-muted text-sm">Disposable song copies for replay while signed in to this account. Kept downloads are separate. Your browser chooses storage and may reclaim it.</p><div class="appearance-controls"><label>Cache budget <select data-budget aria-label="Playback cache budget"><option value="0">Off</option><option value="128">128 MB</option><option value="512">512 MB</option><option value="1024">1 GB</option></select></label><label>Buffering <select data-buffer aria-label="Playback buffering"><option value="stream">Start streaming immediately</option><option value="complete">Cache whole song before playing</option></select></label><label><input data-prefetch type="checkbox"> Prefetch next queued song</label></div><p data-usage role="status"></p><button type="button" class="btn btn-ghost" data-cancel>Cancel transfer</button> <button type="button" class="btn btn-ghost" data-clear>Clear cache</button><details><summary>Cached songs</summary><ul data-items></ul></details>';
  host.appendChild(panel);
  let message = '';
  function render() {
    const p=cachePreferences(), rows=scope() ? Object.values(index(scope())) : [];
    panel.querySelector('[data-budget]').value=p.budget; panel.querySelector('[data-buffer]').value=p.buffer; panel.querySelector('[data-prefetch]').checked=p.prefetch;
    panel.querySelector('[data-usage]').textContent=message || cacheStatus || `${(rows.reduce((s,r)=>s+r.size+(r.lyricsSize || 0),0)/1048576).toFixed(1)} MB · ${rows.length} songs${transfer ? ` · Caching ${transfer.title}…` : ''}${!globalThis.caches ? ' · Cache storage unavailable. Streaming remains available.' : ''}`;
    panel.querySelector('[data-cancel]').disabled=!transfer;
    const list=panel.querySelector('[data-items]'); list.replaceChildren();
    for (const row of rows) { const li=document.createElement('li'); li.textContent=`${row.media.music?.title || row.media.file_name || 'Song'} · ${(row.size/1048576).toFixed(1)} MB`; list.appendChild(li); }
    if (!rows.length) { const li=document.createElement('li'); li.textContent='No cached songs.'; list.appendChild(li); }
  }
  panel.addEventListener('change', async () => {
    cancelCacheTransfer(); message=''; cacheStatus='';
    try { localStorage.setItem(preferencesKey,JSON.stringify({budget:Number(panel.querySelector('[data-budget]').value),buffer:panel.querySelector('[data-buffer]').value,prefetch:panel.querySelector('[data-prefetch]').checked})); if(scope()) await exclusive(()=>prune(scope(),cachePreferences().budget*1048576)); }
    catch { message='Cache settings could not be saved. Check browser storage.'; } render();
  });
  panel.querySelector('[data-cancel]').addEventListener('click',cancelCacheTransfer);
  panel.querySelector('[data-clear]').addEventListener('click',async()=>{ cancelCacheTransfer(); try { const name=scope(); if(name) await exclusive(async()=>{await caches.delete(name);localStorage.removeItem(indexKey(name));});message='Cache cleared. Kept downloads and current playback remain available.'; } catch {message='Cache could not be cleared. Try again.';} render(); });
  document.addEventListener('playback-cache:changed',render);
  document.addEventListener('queue:changed',()=>{ if(cachePreferences().prefetch && musicPrefetchAllowed() && !transfer) { const media=nextSong?.(); if(media) void cacheSong(media,undefined,{prefetch:true}).catch(()=>{}); } });
  window.addEventListener('pagehide',releasePlaybackCache);
  window.addEventListener('storage',event=>{ if (event.key === preferencesKey) cancelCacheTransfer(); if (event.key === preferencesKey || event.key?.startsWith(prefix)) render(); }); render();
}
