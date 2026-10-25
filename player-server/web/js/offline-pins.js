import { offlineMusicMode, offlineMusicAccount } from './offline-context.js';
import { state } from './state.js';
import { waitForCompatStream } from './streamSource.js';
import { downloadQuality } from './download-quality.js';
import { snapshotLyrics } from './saved-lyrics.js';

const prefix = 'player-offline-v1-';
let tail = Promise.resolve();
function scope() { const id = offlineMusicMode() ? offlineMusicAccount() : state.user?.id; return id > 0 ? `${prefix}${id}` : null; }
function current(name, signal) { if (scope() !== name || signal?.aborted) throw new DOMException('Canceled', 'AbortError'); }
function read(name) {
 const value = JSON.parse(localStorage.getItem(`${name}:index`) || '{"collections":{},"songs":{}}');
 if (!value.collections || !value.songs) throw Error('Saved downloads index is unavailable.');
 return value;
}
function write(name, value) { localStorage.setItem(`${name}:index`, JSON.stringify(value)); document.dispatchEvent(new Event('offline-pins:changed')); }
function exclusive(action) {
 const run = () => navigator.locks ? navigator.locks.request('player-offline-pins', action) : action();
 const result = tail.then(run, run); tail = result.catch(() => {}); return result;
}
export function pinCollectionKey(collection) { return JSON.stringify([collection.kind, String(collection.key)]); }
export async function offlinePinStatus(collection) {
 const name = scope();
 if (!name || !globalThis.caches) return null;
 const saved = read(name), row = saved.collections[pinCollectionKey(collection)];
 if (!row) return null;
 const cache = await caches.open(name), songs = [];
 for (const url of row.urls) if (saved.songs[url] && await cache.match(url)) songs.push(saved.songs[url]);
 current(name);
 return { ...row, songs, complete: songs.length === row.urls.length && row.urls.length > 0 };
}
export async function pinMusicCollection(collection, title, songs, { signal, profile = downloadQuality(), progress = () => {} } = {}) {
 if (offlineMusicMode()) throw Error('Reconnect and sign in to pin music.');
 const name = scope();
 if (!name || !globalThis.caches) throw Error('Sign in and allow browser storage before pinning music.');
 if (!['album','playlist'].includes(collection.kind)) throw Error('Choose an album or playlist.');
 const seen = new Set();
 songs = songs.filter(s => s.type === 'audio' && Number.isSafeInteger(s.id) && s.id > 0 && !seen.has(s.id) && seen.add(s.id));
 if (!songs.length) throw Error('No songs available.');
 if (!['original','aac128','aac256','mp3192','opus96'].includes(profile)) throw Error('Download quality unavailable. Choose another quality.');
 return exclusive(async () => {
  current(name, signal);
  const cache = await caches.open(name), rows = read(name);
  const urls = [];
  // Existing completed downloads retain their recorded quality, including
  // copies shared by another pinned collection. The choice applies to new files.
  for (const song of songs) {
   const existing = Object.values(rows.songs).filter(row => row.media.id === song.id);
   let retained;
   for (const row of existing) if (await cache.match(row.url)) { retained = row.url; break; }
   urls.push(retained || new URL(`/api/media/${song.id}/${profile === 'original' ? 'download' : `compat?profile=${encodeURIComponent(profile)}`}`, location.origin).href);
  }
  current(name, signal);
  const key = pinCollectionKey(collection);
  // Refresh changes the playable snapshot, but keeps ownership of every copy
  // this collection saved. An interrupted refresh and a later Remove download
  // can therefore still reclaim superseded members without touching overlaps.
  const previous = rows.collections[key];
  const ownedURLs = [...new Set([...(previous?.ownedURLs || previous?.urls || []), ...urls])];
  rows.collections[key] = { title, urls, ownedURLs, pinned: true };
  write(name, rows);
  let completed = 0;
  for (let i = 0; i < songs.length; i++) {
   current(name, signal);
   const url = urls[i], media = songs[i];
   if (!rows.songs[url] || !await cache.match(url)) {
    if (/\/compat(?:\?|$)/.test(url)) {
     const result = await waitForCompatStream(url, {signal});
     if (result.state !== 'ready') throw Error(result.reason || 'Download canceled.');
    }
    current(name, signal);
    const response = await fetch(url, {credentials:'same-origin',redirect:'manual',cache:'no-store',signal});
    const contentType = response.headers.get('content-type')?.split(';')[0];
    const mediaType = contentType?.startsWith('audio/') || (profile === 'original' && contentType === 'application/octet-stream' && response.headers.get('content-disposition')?.startsWith('attachment'));
    if (response.status !== 200 || !response.body || !mediaType) throw Error(`Could not save song (HTTP ${response.status}). Check access and retry.`);
    const expectedType = {aac128:'audio/mp4',aac256:'audio/mp4',mp3192:'audio/mpeg',opus96:'audio/ogg'}[profile];
    if (expectedType && contentType !== expectedType) throw Error('Server returned a different download format. Choose another quality or retry.');
    const expected = Number(response.headers.get('content-length')); let size = 0;
    const body = response.body.pipeThrough(new TransformStream({transform(chunk, controller) { current(name,signal); size += chunk.byteLength; controller.enqueue(chunk); }}));
    try {
     await cache.put(url, new Response(body,{headers:response.headers}));
     current(name,signal);
     if (!size || (expected > 0 && expected !== size)) throw Error('Incomplete download. Retry.');
     rows.songs[url] = {url,media,size,profile,contentType,revision:globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random()}`,etag:response.headers.get('etag') || ''};
     Object.assign(rows.songs[url],await snapshotLyrics(media.id,url,rows.songs[url].etag,signal,cache));
     current(name,signal);
     write(name, rows);
    } catch(error) { await cache.delete(url); if(rows.songs[url]?.lyricsKey)await cache.delete(rows.songs[url].lyricsKey); delete rows.songs[url]; throw error; }
   }
   progress(++completed, songs.length);
  }
  current(name, signal);
  return offlinePinStatus(collection);
 });
}
export async function unpinMusicCollection(collection, { remove = false } = {}) {
 const name = scope(); if (!name) return;
 return exclusive(async () => {
  current(name); const rows = read(name), key = pinCollectionKey(collection), row = rows.collections[key];
  if (!row) return;
  if (!remove) { row.pinned = false; write(name,rows); return; }
  delete rows.collections[key];
  // Commit references before deleting bytes: interrupted removal leaves harmless
  // unused files, never destroys another collection's saved audio.
  write(name,rows);
  const referenced = new Set(Object.values(rows.collections).flatMap(r => r.ownedURLs || r.urls));
  const cache = await caches.open(name);
  for (const url of row.ownedURLs || row.urls) if (!referenced.has(url)) { await cache.delete(url); if(rows.songs[url]?.lyricsKey)await cache.delete(rows.songs[url].lyricsKey); delete rows.songs[url]; }
  write(name,rows);
 });
}
export async function offlinePinnedDetail(id) {
 const name = scope(); if (!name || !globalThis.caches) return null;
 const cache = await caches.open(name);
 for (const row of Object.values(read(name).songs)) {
  if (String(row.media.id) === String(id) && await cache.match(row.url)) { current(name); return { media:{...row.media,offlinePinURL:row.url}, progress:null }; }
 }
 return null;
}
// A saved rendition is reused only for its exact URL. HTTP refusal removes the
// bytes, while a transport failure permits offline replay for this same account.
export async function offlinePinnedResponse(url, signal) {
 const name = scope(); if (!name || !globalThis.caches) return null;
 url = new URL(url,location.origin).href;
 const rows = read(name), row = rows.songs[url]; if (!row) return null;
 const cache = await caches.open(name), response = await cache.match(url); if (!response) return null;
 if (offlineMusicMode()) { current(name,signal); return response; }
 let check;
 try { check = await fetch(url,{method:'HEAD',credentials:'same-origin',redirect:'manual',cache:'no-store',signal}); }
 catch(error) { if (!(error instanceof TypeError)) throw error; }
 current(name,signal);
 if (check && (!check.ok || (row.etag && check.headers.get('etag') !== row.etag))) {
  if ([401,403,404,410].includes(check.status) || check.ok) {
   await exclusive(async () => {
    current(name,signal);
    const latest = read(name);
    // A delayed HEAD belongs to the copy observed before the request. Never
    // delete its replacement or publish that old manifest over newer mutations.
    if (JSON.stringify(latest.songs[url]) !== JSON.stringify(row)) return;
    await cache.delete(url); if(latest.songs[url]?.lyricsKey)await cache.delete(latest.songs[url].lyricsKey); delete latest.songs[url]; write(name,latest);
   });
  }
  return null;
 }
 return response;
}
export async function offlineSavedCollections() {
 const name=scope(); if(!name || !globalThis.caches)return [];
 const result=[];
 for(const key of Object.keys(read(name).collections)) {
  const [kind,id]=JSON.parse(key), collection={kind,key:id};
  const row=await offlinePinStatus(collection);
  if(row)result.push({...row,collection});
 }
 current(name); return result;
}

// Enumerate only existing saved download indexes for this origin. Account IDs
// are labels for local snapshots, never evidence of an authenticated user.
export function offlineSavedAccounts() {
 const ids = [];
 for (let i = 0; i < localStorage.length; i++) {
  const match = localStorage.key(i)?.match(/^player-offline-v1-([1-9]\d*):index$/);
  if (match && Number.isSafeInteger(Number(match[1]))) ids.push(Number(match[1]));
 }
 return ids.sort((a,b) => a-b);
}
export async function offlineSavedSongs() {
 const name=scope(); if (!name || !globalThis.caches) return [];
 const cache=await caches.open(name), songs=[];
 for (const row of Object.values(read(name).songs)) {
  if (row.media?.type === 'audio' && await cache.match(row.url)) songs.push(row);
 }
 current(name); return songs;
}
