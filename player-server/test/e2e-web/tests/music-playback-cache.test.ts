/** M45 browser acceptance against an exclusively owned disposable music fixture.
 * The added WAVs are complete, decodable originals served by Player. No route
 * fulfills a successful media response from fabricated bytes. */
import { test, expect, request, type Page } from '@playwright/test';
import { createServer, request as httpRequest, type Server } from 'node:http';
import { createHash } from 'node:crypto';
import { existsSync, mkdirSync, openSync, writeSync, closeSync, readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { execFileSync } from 'node:child_process';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { expectPlaying, captureOutput, assertValidCapture, mainSegment } from './helpers/music-ui';

test.skip(process.env.PLAYER_M45_ISOLATED !== '1', 'Requires an exclusively owned disposable fixture');
test.use({ serviceWorkers: 'block' });
const folder = 'M45 Cache Acceptance';
const names = ['01 - Cache A.wav', '02 - Cache B.wav'];
const labels = ['01 - Cache A', '02 - Cache B'];
const attempt = `${Date.now()}-${process.pid}`;
let fixture: MusicFixture, evidence: Evidence, proxy: Server, origin: string;
let ids: number[] = [], mode: 'normal' | 'slow' = 'normal';
let slowPath = '', upstreamClosed = 0;
const expected = new Map<string, string>();

function makeWav(path: string, hz: number): void {
  if (existsSync(path)) return;
  const samples = (69 * 1048576 - 44) / 2, sampleRate = 16000;
  const header = Buffer.alloc(44);
  header.write('RIFF'); header.writeUInt32LE(samples * 2 + 36, 4);
  header.write('WAVEfmt ', 8); header.writeUInt32LE(16, 16);
  header.writeUInt16LE(1, 20); header.writeUInt16LE(1, 22);
  header.writeUInt32LE(sampleRate, 24); header.writeUInt32LE(sampleRate * 2, 28);
  header.writeUInt16LE(2, 32); header.writeUInt16LE(16, 34);
  header.write('data', 36); header.writeUInt32LE(samples * 2, 40);
  const wave = Buffer.alloc(32000);
  for (let i = 0; i < sampleRate; i++) wave.writeInt16LE(Math.round(Math.sin(2 * Math.PI * hz * i / sampleRate) * 8000), i * 2);
  const fd = openSync(path, 'w');
  try {
    writeSync(fd, header);
    let left = samples * 2;
    while (left > 0) { const n = Math.min(left, wave.length); writeSync(fd, wave, 0, n); left -= n; }
  } finally { closeSync(fd); }
}

async function addTracks(): Promise<void> {
  const state = JSON.parse(readFileSync(join(fixture.dir, 'state.json'), 'utf8'));
  const target = join(state.media_root, 'Music', folder); mkdirSync(target, { recursive: true });
  names.forEach((name, i) => {
    const path = join(target, name); makeWav(path, i ? 550 : 440);
    expected.set(name, createHash('sha256').update(readFileSync(path)).digest('hex'));
  });
  const admin = await request.newContext({ baseURL: fixture.origin });
  try {
    expect((await admin.post('/api/v1/auth/login', { data: fixture.admin })).ok()).toBe(true);
    expect((await admin.post('/api/v1/admin/rescan', { data: {} })).ok()).toBe(true);
    await expect.poll(async () => (await (await admin.get('/api/v1/admin/scan-progress')).json()).running, { timeout: 60_000 }).toBe(false);
    const list = await (await admin.get('/api/v1/media?limit=1000')).json();
    ids = names.map(name => list.find((row: any) => row.file_name === name && row.set_id === fixture.setIds.Music)?.id);
    expect(ids.every(Number.isSafeInteger)).toBe(true);
  } finally { await admin.dispose(); }
}

test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  const address = new URL(fixture.origin);
  if (address.protocol !== 'http:' || !['127.0.0.1', 'localhost'].includes(address.hostname) || !existsSync(join(fixture.dir, '.player-music-fixture')))
    throw Error('M45 requires a disposable local Player fixture');
  await addTracks();
  const root = join(resolve(process.env.PLAYER_M45_EVIDENCE || join(tmpdir(), 'player-m45')), attempt);
  evidence = new Evidence(root, info.project.name, {
    browser_version: browser.version(), channel: info.project.use.channel,
    server_version: fixture.serverVersion, server_revision: fixture.buildRevision,
    checkout: execFileSync('git', ['rev-parse', 'HEAD']).toString().trim(), origin: fixture.origin,
  });
  proxy = createServer((incoming, outgoing) => {
    const path = incoming.url || '/';
    const slow = mode === 'slow' && path.includes(slowPath) && incoming.method === 'GET';
    const upstream = httpRequest(new URL(path, fixture.origin), {
      method: incoming.method,
      headers: { ...incoming.headers, host: address.host, 'accept-encoding': 'identity' },
    }, response => {
      outgoing.writeHead(response.statusCode || 502, response.headers);
      if (!slow) { response.pipe(outgoing); return; }
      void (async () => {
        try {
          for await (const chunk of response) {
            if (outgoing.destroyed) break;
            outgoing.write(chunk);
            await new Promise(r => setTimeout(r, 60));
          }
          outgoing.end();
        } catch { outgoing.destroy(); }
      })();
    });
    upstream.on('error', () => { upstreamClosed++; outgoing.destroy(); });
    outgoing.on('close', () => upstream.destroy());
    incoming.pipe(upstream);
  });
  await new Promise<void>(r => proxy.listen(0, '127.0.0.1', r));
  const bound = proxy.address(); if (!bound || typeof bound === 'string') throw Error('Proxy did not bind');
  origin = `http://127.0.0.1:${bound.port}`;
});

test.afterAll(async () => {
  if (proxy) { proxy.closeAllConnections(); await new Promise<void>(r => proxy.close(() => r())); }
});

async function login(page: Page, who: 'user' | 'admin'): Promise<void> {
  await page.goto(origin + '/login.html');
  await expect(page.locator('#username')).toBeFocused();
  await page.keyboard.type(fixture[who].username);
  await page.keyboard.press('Tab');
  await page.keyboard.type(fixture[who].password);
  await page.keyboard.press('Enter');
  await page.waitForURL(url => url.pathname === '/', { waitUntil: 'domcontentloaded' });
  await expect(page.getByRole('button', { name: 'Settings', exact: true })).toBeVisible();
}
async function browse(page: Page): Promise<void> {
  await page.goto(`${origin}/?set=${fixture.setIds.Music}&folder=${encodeURIComponent(folder)}`);
  await expect(page.getByRole('button', { name: labels[0], exact: true })).toBeVisible();
}
async function play(page: Page, index: number): Promise<void> {
  const card = page.locator('.media-card').filter({ has: page.getByRole('button', { name: labels[index], exact: true }) });
  await card.hover(); await card.getByRole('button', { name: 'Play', exact: true }).click();
  await expectPlaying(page, ids[index]);
}
async function settings(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await expect(page.locator('#settings-modal')).toHaveClass(/open/);
  await expect(page.locator('#settings-modal')).toContainText('Kept downloads are separate');
}
async function rows(page: Page): Promise<any[]> {
  return page.evaluate(() => {
    const key = Object.keys(localStorage).find(k => /^player-playback-v1-\d+:index$/.test(k));
    return key ? Object.values(JSON.parse(localStorage.getItem(key) || '{}')) : [];
  });
}
async function cachedHash(page: Page, index: number): Promise<string | null> {
  return page.evaluate(async id => {
    const key = Object.keys(localStorage).find(k => /^player-playback-v1-\d+:index$/.test(k));
    if (!key) return null;
    const entry: any = Object.values(JSON.parse(localStorage.getItem(key) || '{}')).find((r: any) => r.media.id === id);
    if (!entry) return null;
    const response = await (await caches.open(key.slice(0, -6))).match(entry.url);
    if (!response) return null;
    const digest = await crypto.subtle.digest('SHA-256', await response.arrayBuffer());
    return [...new Uint8Array(digest)].map(n => n.toString(16).padStart(2, '0')).join('');
  }, ids[index]);
}

test('M45 complete streamed bytes, offline replay, eviction, prefetch, cancel, and account isolation', async ({ page, context }) => {
  test.setTimeout(300_000);
  const artifacts: string[] = [], proof: Record<string, unknown> = {};
  try {
    await page.addInitScript(installAudioTap);
    await login(page, 'user'); await browse(page);
    await settings(page);
    const panel = page.locator('#settings-modal');
    await panel.getByLabel('Playback cache budget').selectOption('128');
    await panel.getByLabel('Playback buffering').selectOption('complete');
    await panel.getByLabel('Prefetch next queued song').uncheck();
    await page.keyboard.press('Escape');
    await expect(page.getByRole('button', { name: 'Settings', exact: true })).toBeFocused();
    await play(page, 0);
    await expect.poll(() => cachedHash(page, 0), { timeout: 60_000 }).toBe(expected.get(names[0]));
    proof.first_hash = await cachedHash(page, 0);
    await context.setOffline(true);
    if (await page.locator('#btn-minimize').isVisible()) await page.locator('#btn-minimize').click();
    await play(page, 0);
    expect((await page.locator('#media-audio').evaluate((a: HTMLAudioElement) => a.currentSrc)).startsWith('blob:')).toBe(true);
    const sound = await captureOutput(page, 1400, evidence.path('offline-replay.wav'));
    assertValidCapture(sound);
    expect(Math.abs(mainSegment(sound.analysis)!.tone_hz! - 440)).toBeLessThan(6);
    proof.offline_audio = mainSegment(sound.analysis); artifacts.push(sound.wav);
    await context.setOffline(false);
    if (await page.locator('#btn-minimize').isVisible()) await page.locator('#btn-minimize').click();
    await play(page, 1);
    await expect.poll(() => cachedHash(page, 1), { timeout: 60_000 }).toBe(expected.get(names[1]));
    await expect.poll(() => rows(page)).toHaveLength(1);
    expect(await cachedHash(page, 0)).toBeNull();
    proof.evicted = ids[0]; proof.retained = ids[1];
    await settings(page);
    await expect(panel.locator('[data-usage]')).toContainText('1 songs');
    await panel.locator('details').filter({ hasText: 'Cached songs' }).locator('summary').click();
    await expect(panel.locator('[data-items]')).toContainText(labels[1]);
    await page.setViewportSize({ width: 390, height: 844 });
    for (const theme of ['dark', 'light']) {
      await panel.locator('#appearance-mode').selectOption(theme);
      const shot = evidence.path(`settings-${theme}-narrow.png`);
      await page.screenshot({ path: shot, animations: 'disabled' }); artifacts.push(shot);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    }
    // Installed desktop browsers commonly report an unknown connection,
    // whose Mobile policy deliberately leaves prefetch disabled.
    await panel.getByLabel('Music network', { exact: true }).selectOption('wifi');
    await panel.getByLabel('Prefetch next queued song').check();
    await page.keyboard.press('Escape');
    // The visible Play next action changes the queue; the enabled policy then
    // fetches a full copy of the upcoming original without playing it.
    const second = page.locator('.media-card').filter({ has: page.getByRole('button', { name: labels[1], exact: true }) });
    await second.click();
    await page.keyboard.press('a');
    await page.getByRole('group', { name: 'Media actions' }).getByRole('button', { name: 'Play now', exact: true }).click();
    await expectPlaying(page, ids[1]);
    const first = page.locator('.media-card').filter({ has: page.getByRole('button', { name: labels[0], exact: true }) });
    await first.click();
    await page.keyboard.press('a');
    await page.getByRole('group', { name: 'Media actions' }).getByRole('button', { name: 'Play next', exact: true }).click();
    await expect.poll(() => cachedHash(page, 0), { timeout: 60_000 }).toBe(expected.get(names[0]));
    proof.prefetched = ids[0];
    await settings(page);
    await panel.getByRole('button', { name: 'Clear cache', exact: true }).click();
    await expect(panel.locator('[data-usage]')).toContainText('Cache cleared');
    expect(await rows(page)).toHaveLength(0);
    await panel.getByLabel('Prefetch next queued song').uncheck();
    await panel.getByLabel('Playback buffering').selectOption('stream');
    await page.keyboard.press('Escape');
    mode = 'slow'; slowPath = `/api/media/${ids[0]}/stream`;
    if (await page.locator('#btn-minimize').isVisible()) await page.locator('#btn-minimize').click();
    await first.hover();
    await first.getByRole('button', { name: 'Play', exact: true }).click();
    await settings(page);
    await expect(panel.getByRole('button', { name: 'Cancel transfer', exact: true })).toBeEnabled();
    await panel.getByRole('button', { name: 'Cancel transfer', exact: true }).click();
    await expect(panel.getByRole('button', { name: 'Cancel transfer', exact: true })).toBeDisabled();
    expect(await cachedHash(page, 0)).toBeNull();
    mode = 'normal'; proof.cancelled = true; proof.upstream_closed = upstreamClosed;
    await page.keyboard.press('Escape');
    // Leave a complete copy for the original account when switching identity.
    // Otherwise an empty cache would make the account check vacuous.
    await play(page, 1);
    await expect.poll(() => cachedHash(page, 1), { timeout: 60_000 }).toBe(expected.get(names[1]));
    await page.getByRole('button', { name: 'Log out', exact: true }).click();
    await page.waitForURL(url => url.pathname === '/login.html');
    await login(page, 'admin'); await browse(page);
    const adminConfig = await (await page.request.get(origin + '/api/config')).json();
    await context.setOffline(true);
    const adminRows = await page.evaluate(userId => Object.values(JSON.parse(
      localStorage.getItem(`player-playback-v1-${userId}:index`) || '{}')),
    adminConfig.user_id);
    expect(adminRows).toHaveLength(0);
    expect(await page.evaluate(async id => {
      const { cachedMediaDetail } = await import('/js/playback-cache.js' as string);
      return cachedMediaDetail(id, () => Promise.reject(new TypeError('offline'))).then(() => false, () => true);
    }, ids[1])).toBe(true);
    const deniedCard = page.locator('.media-card').filter({ has: page.getByRole('button', { name: labels[1], exact: true }) });
    await deniedCard.hover();
    await deniedCard.getByRole('button', { name: 'Play', exact: true }).click();
    await expect.poll(() => page.locator('#media-audio').evaluate((a: HTMLAudioElement) => a.currentSrc.startsWith('blob:'))).toBe(false);
    proof.admin_cache_rows = adminRows.length;
    await context.setOffline(false);
    evidence.record({ feature: 'M45', scenario: 'actual complete source and offline output; 128 MB LRU; queue prefetch; cancellation; account boundary; keyboard, narrow themes', result: 'pass', data: proof, artifacts });
  } catch (error) {
    await context.setOffline(false).catch(() => {});
    evidence.record({ feature: 'M45', scenario: 'browser cache journey', result: 'fail', detail: String(error), data: proof, artifacts });
    throw error;
  }
});
