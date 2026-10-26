/** M46 installed Chrome/Firefox acceptance. All successful media bytes come
 * from the real disposable server. The per-client proxy only slows/truncates
 * responses; it never creates a successful ZIP or mutates fixture media. */
import { test, expect, request, type Download, type Page } from '@playwright/test';
import { createServer, request as httpRequest, type Server } from 'node:http';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { execFileSync } from 'node:child_process';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { audioState, expectPlaying, captureOutput, assertValidCapture, mainSegment } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires disposable local Music fixture');
test.use({ serviceWorkers: 'block' });
let f: MusicFixture, e: Evidence, proxy: Server, origin: string;
let mode: 'normal' | 'slow' | 'truncate' = 'normal';
let transfers: { status: number; mode: string; sent: number; ended: boolean }[] = [];
// Complete successful ZIPs as the page received them. Playwright's Firefox
// channel does not emit a download event for blob: ObjectURL saves, so the
// runner keeps the real upstream bytes after the UI reaches Archive ready.
let lastCompleteZip: Buffer | null = null;
let archivesDelivered = 0;
const attempt = `${Date.now()}-${process.pid}`;
const sha = (bytes: Buffer) => createHash('sha256').update(bytes).digest('hex');
const dialog = (p: Page) => p.getByRole('dialog', { name: 'Download music', exact: true });
const grid = (p: Page) => p.locator('#media-grid');
const sleep = (ms: number) => new Promise(r => setTimeout(r, ms));

test.beforeAll(async ({ browser }, info) => {
 f = loadFixture();
 const endpoint = new URL(f.origin);
 if (endpoint.protocol !== 'http:' || !['127.0.0.1', 'localhost'].includes(endpoint.hostname)) throw Error('Disposable local server required');
 // Never consume the mutable shared fixture evidence_dir (another worker owns it).
 const root = join(resolve(process.env.PLAYER_M46_EVIDENCE || join(tmpdir(), 'player-m46')), attempt);
 const servedModule = await (await fetch(f.origin + '/js/music-downloads.js')).text();
 const servedWorker = await (await fetch(f.origin + '/sw.js')).text();
 e = new Evidence(root, info.project.name, { web_module_sha256: sha(Buffer.from(servedModule)), web_cache: servedWorker.match(/kiss-v\d+/)?.[0], web_checkout: execFileSync('git', ['rev-parse', 'HEAD']).toString().trim(), browser_version: browser.version(), channel: info.project.use.channel,
  server_version: f.serverVersion, server_build_revision: f.buildRevision, checkout: f.checkout, origin: f.origin });
 proxy = createServer((incoming, outgoing) => {
  if (incoming.url === '/__m46_local_player') { outgoing.writeHead(200, { 'Content-Type': 'text/html' }); outgoing.end('<!doctype html><title>M46 local playback</title>'); return; }
  const selected = incoming.url?.match(/^\/api\/(v1\/)?music\/downloads(?:\?|$)/) && incoming.method === 'POST';
  const transferMode = selected ? mode : 'normal';
  const upstream = httpRequest(new URL(incoming.url!, f.origin), {
   method: incoming.method, headers: { ...incoming.headers, host: new URL(f.origin).host, 'accept-encoding': 'identity' },
  }, response => {
   outgoing.writeHead(response.statusCode!, response.headers);
   const record = { status: response.statusCode!, mode: transferMode, sent: 0, ended: false };
   if (selected) transfers.push(record);
   const capture = selected && transferMode === 'normal' && response.statusCode === 200
    && String(response.headers['content-type'] || '').includes('application/zip');
   const parts: Buffer[] = [];
   void (async () => {
    try {
     for await (const raw of response) {
      const chunk = Buffer.from(raw);
      for (let i = 0; i < chunk.length; i += 4096) {
       if (outgoing.destroyed) { response.destroy(); return; }
       const part = chunk.subarray(i, i + 4096);
       outgoing.write(part); record.sent += part.length;
       if (capture) parts.push(part);
       if (transferMode === 'truncate' && record.sent >= 8192) { outgoing.destroy(); response.destroy(); return; }
       if (transferMode === 'slow') await sleep(150);
      }
     }
     record.ended = true; outgoing.end();
     if (capture && parts.length) lastCompleteZip = Buffer.concat(parts);
    } catch { outgoing.destroy(); }
   })();
  });
  upstream.on('error', () => outgoing.destroy());
  outgoing.on('close', () => upstream.destroy());
  incoming.pipe(upstream);
 });
 await new Promise<void>(r => proxy.listen(0, '127.0.0.1', r));
 const address = proxy.address(); if (!address || typeof address === 'string') throw Error('Missing proxy port');
 origin = `http://127.0.0.1:${address.port}`;
});
test.afterAll(async () => {
 if (proxy) { proxy.closeAllConnections(); await new Promise<void>(r => proxy.close(() => r())); }
});
test.beforeEach(async ({ page }) => {
 mode = 'normal'; transfers = []; lastCompleteZip = null; archivesDelivered = 0;
 await page.addInitScript(installAudioTap);
 // keyboardLogin uses relative navigation, so set the already-authorized proxy origin first.
 await page.goto(origin + '/login.html');
 await page.locator('#username').fill(f.user.username);
 await page.locator('#password').fill(f.user.password);
 await page.locator('#password').press('Enter');
 await page.waitForURL(url => url.pathname === '/', { waitUntil: 'domcontentloaded' });
 await expect(page.getByRole('button', { name: 'Toggle sets sidebar', exact: true })).toBeVisible();
});

async function album(p: Page) {
 const list = await (await p.request.get(f.origin + '/api/v1/music/albums')).json();
 const entry = list.find((a: any) => a.title === 'Night Transit'); expect(entry).toBeTruthy();
 const detail = await (await p.request.get(f.origin + '/api/v1/music/albums/' + entry.album_key)).json();
 return { ...entry, songs: detail.songs };
}
async function openDialog(p: Page) {
 const opener = grid(p).getByRole('button', { name: 'Download…', exact: true });
 await opener.focus(); await p.keyboard.press('Enter');
 await expect(dialog(p)).toBeVisible();
 await expect(dialog(p).getByRole('combobox', { name: 'Download quality', exact: true })).toBeEnabled();
 await dialog(p).getByRole('combobox', { name: 'Download quality', exact: true }).selectOption('original');
 return opener;
}
async function download(p: Page, name: string) {
 lastCompleteZip = null;
 let file: Download | null = null;
 const pending = p.waitForEvent('download').then(d => { file = d; }, () => undefined);
 await dialog(p).getByRole('button', { name: /^(Download ZIP|Retry|Download again)$/ }).click();
 await expect(dialog(p).locator('[data-status]')).toContainText('Archive ready:');
 // Chrome usually already emitted download; Firefox blob: saves often never do.
 await Promise.race([pending, sleep(1000)]);
 const path = e.path(name + '.zip');
 if (file) {
  expect(await file.failure()).toBeNull();
  await file.saveAs(path);
 } else {
  await expect.poll(() => lastCompleteZip).toBeTruthy();
  writeFileSync(path, lastCompleteZip!);
 }
 archivesDelivered += 1;
 return path;
}
function verifyZip(path: string, ids: number[]) {
 const state = JSON.parse(readFileSync(join(f.dir, 'state.json'), 'utf8'));
 const expected = [...new Set(ids)].map(id => {
  const relative = Object.entries(f.mediaIds).find(([, value]) => value === id)![0];
  return { id, relative, hash: sha(readFileSync(join(state.media_root, relative))) };
 });
 // stdlib ZIP parser checks CRC as well as each entry's exact original SHA256.
 const extracted = JSON.parse(execFileSync('python3', ['-c',
  'import json,sys,zipfile,hashlib,base64\nwith zipfile.ZipFile(sys.argv[1]) as z:\n print(json.dumps([{ "name":n,"sha256":hashlib.sha256(z.read(n)).hexdigest(),"data":base64.b64encode(z.read(n)).decode()} for n in z.namelist()]))', path],
  { maxBuffer: 16 * 1024 * 1024 }).toString());
 expect(extracted.map((x: any) => Number(x.name.split('-')[0]))).toEqual(expected.map(x => x.id));
 for (const [index, row] of expected.entries()) expect(extracted[index].sha256).toBe(row.hash);
 return extracted.map((x: any, index: number) => ({ ...x, id: expected[index].id }));
}
async function queue(p: Page) {
 return p.evaluate(async () => { const s = (await import('/js/queue.js' as string)).queueState();
  return { ids: s.entries.map((x: any) => x.media.id), occurrences: s.entries.map((x: any) => x.id), index: s.index }; });
}
async function offlineExports(p: Page, entries: any[]) {
 // ZIP export explicitly promises a compatible local player, not an in-app
 // offline library. This local HTML audio player decodes the extracted bytes
 // in the installed browser with networking disabled and measures its output.
 await p.setContent('<h1>Extracted M46 originals — offline local player</h1><audio controls></audio><button>Play extracted file</button>');
 await p.context().setOffline(true);
 const proof: any[] = [];
 try {
  for (const entry of entries) {
   const expected = f.tracks.find(t => f.mediaIds[t.path] === entry.id)!;
   await p.evaluate(({ data, name }) => {
    const audio = document.querySelector('audio')!; audio.pause();
    const extension = name.split('.').pop()!;
    const mime: Record<string, string> = { flac: 'audio/flac', mp3: 'audio/mpeg', m4a: 'audio/mp4', opus: 'audio/ogg', ogg: 'audio/ogg' };
    audio.src = `data:${mime[extension] || 'application/octet-stream'};base64,${data}`;
    document.querySelector('button')!.onclick = async () => {
     const w = window as any;
     w.__m46context ||= new AudioContext();
     if (!w.__m46source) { w.__m46source = w.__m46context.createMediaElementSource(audio); w.__m46source.connect(w.__m46context.destination); }
     await w.__m46context.resume(); await audio.play();
    };
   }, entry);
   await p.getByRole('button', { name: 'Play extracted file', exact: true }).click();
   await expect.poll(() => p.locator('audio').evaluate((a: HTMLAudioElement) => a.currentTime)).toBeGreaterThan(.4);
   const out = await captureOutput(p, 1200, e.path(`offline-export-${entry.id}.wav`)); assertValidCapture(out);
   const segment = mainSegment(out.analysis)!; expect(segment.duration_s).toBeGreaterThan(.7);
   expect(Math.abs(segment.tone_hz! - expected.tone_hz[0])).toBeLessThan(5);
   await p.locator('audio').evaluate((a: HTMLAudioElement) => a.pause());
   proof.push({ id: entry.id, sha256: entry.sha256, audio: segment });
  }
 } finally { await p.context().setOffline(false); }
 return proof;
}

test('M46 album, duplicate playlist and selected collections export exact originals; every saved entry plays offline', async ({ page, context }) => {
 test.setTimeout(180_000);
 const a = await album(page), artifacts: string[] = [], saved = new Map<number, any>();
 const catalog = await (await page.request.get(f.origin + '/api/v1/music/albums')).json();
 const compilation = catalog.find((x: any) => x.title === 'Synth Compilation'); expect(compilation).toBeTruthy();
 const compilationSongs = (await (await page.request.get(f.origin + '/api/v1/music/albums/' + compilation.album_key)).json()).songs;
 const created = await page.request.post(f.origin + '/api/v1/playlists', { data: { name: 'M46 ' + attempt, comment: 'Disposable bulk download acceptance' } });
 expect(created.ok()).toBe(true); const playlist = await created.json();
 const repeated = [a.songs[2].id, a.songs[0].id, a.songs[2].id, a.songs[1].id];
 const added = await page.request.post(f.origin + `/api/v1/playlists/${playlist.playlist_id}/songs`, { data: { action: 'add', media_ids: repeated } }); expect(added.ok()).toBe(true);
 try {
  for (const target of [{ kind: 'album', key: a.album_key, ids: a.songs.map((s: any) => s.id) },
   { kind: 'playlist', key: playlist.playlist_id, ids: repeated }, { kind: 'selection', key: '', ids: [...a.songs, ...compilationSongs].map((s: any) => s.id) }]) {
   await page.goto(origin + (target.kind === 'selection' ? '/?music=albums' : `/?music=${target.kind}&id=${encodeURIComponent(target.key)}`));
   if (target.kind === 'selection') {
    await grid(page).getByRole('button', { name: 'Select albums', exact: true }).click();
    await grid(page).locator(`[data-collection-key="${a.album_key}"]`).getByRole('checkbox').check();
    await grid(page).locator(`[data-collection-key="${compilation.album_key}"]`).getByRole('checkbox').check();
    // Expansion follows displayed collection order.
    const keys = await grid(page).locator('[data-collection-key]').evaluateAll(nodes => nodes.map(n => (n as HTMLElement).dataset.collectionKey));
    target.ids = keys.filter(k => [a.album_key, compilation.album_key].includes(k)).flatMap(k => (k === a.album_key ? a.songs : compilationSongs).map((song: any) => song.id));
   }
   const before = await queue(page), opener = await openDialog(page);
   await expect(dialog(page)).toContainText(`${new Set(target.ids).size} unique songs`);
   const close = dialog(page).getByRole('button', { name: 'Close', exact: true });
   await close.focus(); await page.keyboard.press('Tab');
   await expect(dialog(page).getByRole('button', { name: 'Download ZIP', exact: true })).toBeFocused();
   await page.keyboard.press('Tab'); await expect(dialog(page).getByRole('combobox', { name: 'Download quality', exact: true })).toBeFocused();
   await page.keyboard.press('Shift+Tab'); await expect(dialog(page).getByRole('button', { name: 'Download ZIP', exact: true })).toBeFocused();
   const path = await download(page, target.kind); artifacts.push(path);
   for (const row of verifyZip(path, target.ids)) saved.set(row.id, row);
   expect(await queue(page)).toEqual(before);
   if (target.kind === 'album') for (const [theme, width] of [['dark', 1366], ['light', 390]] as const) {
    await page.setViewportSize({ width, height: 844 });
    await page.evaluate(t => document.documentElement.setAttribute('data-theme', t), theme);
    expect(await dialog(page).evaluate(n => n.getBoundingClientRect().width <= innerWidth)).toBe(true);
    const shot = e.path(`dialog-${theme}-${width}.png`); await page.screenshot({ path: shot }); artifacts.push(shot);
   }
   const focusBeforeEscape = await page.evaluate(() => ({active: document.activeElement?.outerHTML.slice(0, 400), focusInDownload: Boolean(document.activeElement?.closest('[aria-labelledby="music-download-title"]')), width: innerWidth}));
   writeFileSync(e.path(`focus-before-escape-${target.kind}.json`), JSON.stringify(focusBeforeEscape, null, 2));
   await page.keyboard.press('Escape'); await expect(dialog(page)).toHaveCount(0);
   await expect(opener, JSON.stringify(focusBeforeEscape)).toBeFocused();
   await page.setViewportSize({ width: 1366, height: 844 });
  }
  const local = await context.newPage(); await local.addInitScript(installAudioTap); await local.goto(origin + '/__m46_local_player');
  const proof = await offlineExports(local, [...saved.values()]); await local.close();
  e.record({ feature: 'M46', scenario: 'album/playlist/selection ZIP integrity, focus, themes and every exported original offline', result: 'pass', data: { proof }, artifacts });
 } catch (error) { e.record({ feature: 'M46', scenario: 'exports', result: 'fail', detail: String(error), artifacts }); throw error; }
 finally { await context.setOffline(false); await page.request.delete(f.origin + '/api/v1/playlists/' + playlist.playlist_id); }
});

test('M46 actual slow/interrupted transfers show progress, preserve listening, cancel cleanly and retry; real ACL/count denial', async ({ page, context }) => {
 const a = await album(page), artifacts: string[] = [];
 const admin = await request.newContext({ baseURL: f.origin }); await admin.post('/api/v1/auth/login', { data: f.admin });
 const users = await (await admin.get('/api/v1/admin/users')).json();
 const user = users.find((u: any) => u.username === f.user.username);
 const grant = { set_id: f.setIds.Music, user_id: user.id, role: 'viewer' };
 try {
  await page.goto(origin + '/?music=album&id=' + encodeURIComponent(a.album_key));
  await grid(page).getByRole('button', { name: '▶ Play album', exact: true }).click(); await expectPlaying(page, a.songs[0].id);
  await page.keyboard.press('Escape'); const before = await audioState(page), beforeQueue = await queue(page);
  mode = 'slow'; const opener = await openDialog(page);
  await dialog(page).getByRole('button', { name: 'Download ZIP', exact: true }).click();
  await expect(dialog(page).locator('progress')).toBeVisible(); await expect(dialog(page).locator('[data-status]')).toContainText('Downloading archive');
  const during = await audioState(page); expect(during.src).toBe(before.src); expect(during.paused).toBe(false); expect(during.time).toBeGreaterThan(before.time);
  expect(await queue(page)).toEqual(beforeQueue);
  const progress = e.path('slow-progress.png'); await page.screenshot({ path: progress }); artifacts.push(progress);
  await dialog(page).getByRole('button', { name: 'Cancel', exact: true }).click(); await expect(dialog(page)).toHaveCount(0); await expect(opener).toBeFocused();
  expect(archivesDelivered).toBe(0);
  await page.getByRole('button', { name: 'Log out', exact: true }).focus(); await page.keyboard.press('p');
  await expect.poll(async () => (await audioState(page)).paused).toBe(true);
  mode = 'truncate'; await openDialog(page);
  await dialog(page).getByRole('button', { name: 'Download ZIP', exact: true }).click();
  await expect(dialog(page).getByRole('button', { name: 'Retry', exact: true })).toBeEnabled(); expect(archivesDelivered).toBe(0);
  mode = 'normal'; const retry = await download(page, 'retry'); artifacts.push(retry); verifyZip(retry, a.songs.map((s: any) => s.id));
  expect(archivesDelivered).toBe(1); await page.keyboard.press('Escape');
  // Open while allowed, then revoke before the real POST. Restoration is unconditional.
  await openDialog(page);
  expect((await admin.delete('/api/v1/admin/permissions', { data: grant })).ok()).toBe(true);
  try {
   await dialog(page).getByRole('button', { name: 'Download ZIP', exact: true }).click();
   await expect(dialog(page).getByRole('button', { name: 'Retry', exact: true })).toBeEnabled(); expect(archivesDelivered).toBe(1);
   expect(transfers.at(-1)?.status).toBe(403);
  } finally { expect((await admin.post('/api/v1/admin/permissions', { data: grant })).ok()).toBe(true); }
  const restored = await download(page, 'acl-restored'); artifacts.push(restored); verifyZip(restored, a.songs.map((s: any) => s.id));
  await page.keyboard.press('Escape');
  const tooMany = await page.request.post(f.origin + '/api/v1/music/downloads', { data: { media_ids: Array(501).fill(a.songs[0].id), profile: 'original' } });
  expect(tooMany.status()).toBe(400);
  const search = page.getByRole('searchbox', { name: 'Search media', exact: true });
  await page.keyboard.press('/'); await search.fill('j k p / ?'); await expect(search).toHaveValue('j k p / ?'); await page.keyboard.press('Escape');
  // Browser navigation destroys the old in-flight export before a different
  // account signs in through the visible form; no old ZIP may be delivered.
  const beforeSwitch = archivesDelivered; mode = 'slow';
  await page.goto(origin + '/?music=album&id=' + encodeURIComponent(a.album_key)); await openDialog(page);
  await dialog(page).getByRole('button', { name: 'Download ZIP', exact: true }).click();
  await expect(dialog(page).locator('progress')).toBeVisible();
  // Drop the session so login.html stays on the form (an active cookie redirects
  // straight home). Navigation plus cookie loss also aborts the in-flight export.
  await page.context().clearCookies();
  await page.goto(origin + '/login.html');
  await page.locator('#username').fill(f.admin.username); await page.locator('#password').fill(f.admin.password); await page.locator('#password').press('Enter');
  await page.waitForURL(url => url.pathname === '/', { waitUntil: 'domcontentloaded' });
  await expect(page.getByRole('button', { name: 'Toggle sets sidebar', exact: true })).toBeVisible();
  await page.waitForTimeout(300); expect(archivesDelivered).toBe(beforeSwitch);
  e.record({ feature: 'M46', scenario: 'real transfer cancel/interruption/retry, progress/listening, ACL, account navigation and 501-input denial', result: 'pass', data: { transfers, archivesDelivered, before, during }, artifacts });
  e.record({ feature: 'M46', scenario: '512 MiB real archive and >500 unique songs visible collection boundary', result: 'gap', proof: 'none', detail: 'Current fixture is small; no synthetic successful payload or 512 MiB file generated by this runner.' });
 } catch (error) { e.record({ feature: 'M46', scenario: 'transfer boundaries', result: 'fail', detail: String(error), data: { transfers, archivesDelivered }, artifacts }); throw error; }
 finally { mode = 'normal'; await admin.post('/api/v1/admin/permissions', { data: grant }); await admin.dispose(); await context.setOffline(false); }
});
