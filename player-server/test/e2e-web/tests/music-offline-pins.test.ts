/** M47 real browser collection pins on an isolated disposable music fixture. */
import { test, expect, request, type Page } from '@playwright/test';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, expectPlaying, captureOutput, assertValidCapture, mainSegment, openCollectionMoreAction } from './helpers/music-ui';

test.skip(process.env.PLAYER_M47_ISOLATED !== '1', 'Requires an isolated disposable music fixture');
const attempt = `${Date.now()}-${process.pid}`;
let f: MusicFixture, e: Evidence;
const title = 'Night Transit';
const sha = (bytes: Buffer) => createHash('sha256').update(bytes).digest('hex');

async function pinRows(page: Page): Promise<{ collection: any; songs: any[]; hashes: Record<number, string> }> {
  return page.evaluate(async () => {
    const key = Object.keys(localStorage).find(k => /^player-offline-v1-\d+:index$/.test(k));
    if (!key) throw Error('No saved collection index');
    const index = JSON.parse(localStorage.getItem(key)!);
    const cache = await caches.open(key.slice(0, -6));
    const hashes: Record<number, string> = {};
    for (const row of Object.values(index.songs) as any[]) {
      const response = await cache.match(row.url);
      if (!response) continue;
      const digest = await crypto.subtle.digest('SHA-256', await response.arrayBuffer());
      hashes[row.media.id] = [...new Uint8Array(digest)].map(n => n.toString(16).padStart(2, '0')).join('');
    }
    return { collection: index.collections, songs: Object.values(index.songs), hashes };
  });
}

async function playbackRows(page: Page): Promise<any[]> {
  return page.evaluate(() => {
    const key = Object.keys(localStorage).find(k => /^player-playback-v1-\d+:index$/.test(k));
    return key ? Object.values(JSON.parse(localStorage.getItem(key) || '{}')) : [];
  });
}

async function openPin(page: Page, name: string): Promise<void> {
  await openCollectionMoreAction(page, /Pin offline|Manage…/);
  await expect(page.getByRole('dialog', { name, exact: true })).toBeVisible();
  await expect(page.getByRole('dialog', { name, exact: true }).locator('h2')).toBeFocused();
}

async function savedHash(page: Page, id: number): Promise<string | undefined> {
  return (await pinRows(page)).hashes[id];
}

test.beforeAll(async ({ browser }, info) => {
  f = loadFixture();
  const state = JSON.parse(readFileSync(join(f.dir, 'state.json'), 'utf8'));
  if (!new URL(f.origin).hostname.match(/^(localhost|127\.0\.0\.1)$/) || !existsSync(join(f.dir, '.player-music-fixture')))
    throw Error('M47 requires its own disposable localhost fixture');
  const served = await (await fetch(f.origin + '/js/offline-pins.js')).text();
  const root = join(resolve(process.env.PLAYER_M47_EVIDENCE || join(tmpdir(), 'player-m47-web-evidence')), attempt);
  e = new Evidence(root, info.project.name, {
    browser_version: browser.version(), channel: info.project.use.channel,
    server_version: f.serverVersion, server_revision: f.buildRevision,
    checkout: execFileSync('git', ['rev-parse', 'HEAD']).toString().trim(),
    offline_module_sha256: sha(Buffer.from(served)), origin: f.origin,
    media_root: state.media_root,
  });
});

test('M47 album and playlist pins survive cache eviction, restart and offline audio; Unpin keeps files and Remove deletes only local copies', async ({ page, context }) => {
  test.setTimeout(240_000);
  const artifacts: string[] = [];
  let playlistId = '';
  try {
    await page.addInitScript(installAudioTap);
    await keyboardLogin(page, f.user);
    const albums = await (await page.request.get('/api/v1/music/albums')).json();
    const album = albums.find((row: any) => row.title === title);
    const synth = albums.find((row: any) => row.title === 'Synth Compilation');
    expect(album && synth).toBeTruthy();
    const albumSongs = (await (await page.request.get(`/api/v1/music/albums/${album.album_key}`)).json()).songs;
    const synthSongs = (await (await page.request.get(`/api/v1/music/albums/${synth.album_key}`)).json()).songs;
    expect(albumSongs).toHaveLength(3);
    const playlistName = `M47 saved overlap ${attempt}`;
    const created = await page.request.post('/api/v1/playlists', { data: { name: playlistName } });
    expect(created.ok()).toBe(true);
    playlistId = (await created.json()).playlist_id;
    expect((await page.request.post(`/api/v1/playlists/${playlistId}/songs`, {
      data: { action: 'add', media_ids: [albumSongs[0].id, synthSongs[0].id, albumSongs[0].id] },
    })).ok()).toBe(true);
    const expected = new Map<number, string>();
    for (const media of [...albumSongs, synthSongs[0]]) {
      const relative = Object.entries(f.mediaIds).find(([, id]) => id === media.id)?.[0];
      expect(relative).toBeTruthy();
      const state = JSON.parse(readFileSync(join(f.dir, 'state.json'), 'utf8'));
      expected.set(media.id, sha(readFileSync(join(state.media_root, relative!))));
    }

    await page.goto(`/?music=album&id=${encodeURIComponent(album.album_key)}`);
    await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible();
    await openPin(page, title);
    const albumDialog = page.getByRole('dialog', { name: title, exact: true });
    await albumDialog.getByRole('combobox', { name: 'Download quality' }).selectOption('original');
    await albumDialog.getByRole('button', { name: 'Pin offline', exact: true }).click();
    await expect(albumDialog.locator('[data-status]')).toContainText('Pinned · 3 of 3', { timeout: 60_000 });
    const albumShot = e.path('m47-album-pinned.png'); await page.screenshot({ path: albumShot, fullPage: true }); artifacts.push(albumShot);
    for (const media of albumSongs) expect(await savedHash(page, media.id)).toBe(expected.get(media.id));
    await page.keyboard.press('Escape');
    await expect(page.locator('#media-grid').getByRole('button', { name: 'More', exact: true })).toBeFocused();
    await expect(page.locator('#media-grid [data-collection-pin]')).toHaveText(/Pinned 3\/3 · Manage…/);
    await page.reload();
    await expect(page.locator('#media-grid [data-collection-pin]')).toHaveText(/Pinned 3\/3 · Manage…/);
    await page.locator('#media-grid').getByRole('button', { name: 'More', exact: true }).click();
    await expect(page.getByRole('menu', { name: 'More collection actions' }).getByRole('button', { name: /Pinned 3\/3 · Manage…/ })).toBeVisible();
    await page.keyboard.press('Escape');

    await page.goto(`/?music=playlist&id=${encodeURIComponent(playlistId)}`);
    await expect(page.getByRole('heading', { name: playlistName, exact: true })).toBeVisible();
    await openPin(page, playlistName);
    const playlistDialog = page.getByRole('dialog', { name: playlistName, exact: true });
    await playlistDialog.getByRole('combobox', { name: 'Download quality' }).selectOption('original');
    await playlistDialog.getByRole('button', { name: 'Pin offline', exact: true }).click();
    await expect(playlistDialog.locator('[data-status]')).toContainText('Pinned · 2 of 2', { timeout: 60_000 });
    expect(Object.keys((await pinRows(page)).hashes)).toHaveLength(4);
    expect(await savedHash(page, synthSongs[0].id)).toBe(expected.get(synthSongs[0].id));
    await playlistDialog.getByRole('button', { name: 'Close', exact: true }).click();

    // Fill the disposable cache through an ordinary Play action, then evict it
    // through Settings. The source is outside both pinned collections.
    await page.getByRole('button', { name: 'Settings', exact: true }).click();
    const settings = page.locator('#settings-modal');
    await settings.getByLabel('Playback cache budget').selectOption('128');
    await settings.getByLabel('Playback buffering').selectOption('complete');
    await page.keyboard.press('Escape');
    await page.goto(`/?set=${f.setIds.Music}&folder=${encodeURIComponent('Gain Check')}`);
    const card = page.locator('.media-card').filter({ has: page.getByRole('button', { name: 'No Gain Tags', exact: true }) });
    await card.hover(); await card.getByRole('button', { name: 'Play', exact: true }).click();
    const unpinned = f.mediaIds['Music/Gain Check/No Gain Tags.mp3'];
    await expectPlaying(page, unpinned);
    await expect.poll(async () => (await playbackRows(page)).length, { timeout: 60_000 }).toBeGreaterThan(0);
    await page.getByRole('button', { name: 'Settings', exact: true }).click();
    await settings.getByRole('button', { name: 'Clear cache', exact: true }).click();
    await expect(settings.locator('[data-usage]')).toContainText('Cache cleared');
    expect(await playbackRows(page)).toHaveLength(0);
    for (const [id, hash] of expected) expect(await savedHash(page, id)).toBe(hash);
    await page.keyboard.press('Escape');

    await expect.poll(() => page.evaluate(() => Boolean(navigator.serviceWorker.controller)), { timeout: 20_000 }).toBe(true);
    await context.setOffline(true);
    await page.goto('/index.html?offline=music');
    await expect(page.getByRole('heading', { name: 'Offline music' })).toBeVisible();
    await page.getByLabel('Browse').selectOption('Albums');
    await expect(page.getByRole('button', { name: title, exact: true })).toBeVisible();
    await page.getByRole('button', { name: title, exact: true }).focus();
    await page.keyboard.press('Enter');
    await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Play now', exact: true }).click();
    await expectPlaying(page, albumSongs[0].id);
    const sound = await captureOutput(page, 1500, e.path('m47-pinned-offline.wav'));
    assertValidCapture(sound);
    expect(Math.abs((mainSegment(sound.analysis)?.tone_hz || 0) - 440)).toBeLessThan(6);
    artifacts.push(sound.wav);
    const offlineShot = e.path('m47-offline-album.png'); await page.screenshot({ path: offlineShot, fullPage: true }); artifacts.push(offlineShot);
    await context.setOffline(false);

    await page.goto(`/?music=album&id=${encodeURIComponent(album.album_key)}`);
    await openPin(page, title);
    await albumDialog.getByRole('button', { name: 'Unpin', exact: true }).click();
    await expect(albumDialog.locator('[data-status]')).toContainText('Unpinned · 3 of 3');
    for (const media of albumSongs) expect(await savedHash(page, media.id)).toBe(expected.get(media.id));
    page.once('dialog', d => d.accept());
    await albumDialog.getByRole('button', { name: 'Remove download…' }).click();
    await expect(albumDialog.locator('[data-status]')).toContainText('Not pinned');
    const afterAlbumRemoval = (await pinRows(page)).hashes;
    expect(Object.keys(afterAlbumRemoval).map(Number).sort()).toEqual([albumSongs[0].id, synthSongs[0].id].sort());
    expect((await page.request.get(`/api/v1/music/albums/${album.album_key}`)).ok()).toBe(true);
    await albumDialog.getByRole('button', { name: 'Close', exact: true }).click();
    await page.goto(`/?music=playlist&id=${encodeURIComponent(playlistId)}`);
    await openPin(page, playlistName);
    page.once('dialog', d => d.accept());
    await playlistDialog.getByRole('button', { name: 'Remove download…' }).click();
    await expect(playlistDialog.locator('[data-status]')).toContainText('Not pinned');
    expect(Object.keys((await pinRows(page)).hashes)).toHaveLength(0);
    expect((await page.request.get(`/api/v1/playlists/${playlistId}`)).ok()).toBe(true);

    e.record({ feature: 'M47', scenario: 'album and overlapping playlist pin, exact original bytes, disposable cache eviction, offline decoded audio, restart, Unpin and local Remove', result: 'pass',
      data: { album_song_ids: albumSongs.map((s: any) => s.id), playlist_song_ids: [albumSongs[0].id, synthSongs[0].id], offline_audio: mainSegment(sound.analysis), retained_overlap: albumSongs[0].id }, artifacts });
  } catch (error) {
    e.record({ feature: 'M47', scenario: 'browser collection pin acceptance', result: 'fail', detail: String(error), artifacts });
    throw error;
  } finally {
    if (playlistId) await page.request.delete(`/api/v1/playlists/${playlistId}`).catch(() => {});
  }
});

test('M47 saved collections stay account scoped and server denial cannot play old bytes', async ({ page }) => {
  test.setTimeout(120_000);
  const artifacts: string[] = [];
  const admin = await request.newContext({ baseURL: f.origin });
  let revoked = false;
  try {
    await page.addInitScript(installAudioTap);
    await keyboardLogin(page, f.user);
    expect((await admin.post('/api/v1/auth/login', { data: f.admin })).ok()).toBe(true);
    const users = await (await admin.get('/api/v1/admin/users')).json();
    const userId = users.find((row: any) => row.username === f.user.username)?.id;
    expect(userId).toBeGreaterThan(0);
    const albums = await (await page.request.get('/api/v1/music/albums')).json();
    const album = albums.find((row: any) => row.title === title);
    await page.goto(`/?music=album&id=${encodeURIComponent(album.album_key)}`);
    await openPin(page, title);
    const pin = page.getByRole('dialog', { name: title, exact: true });
    await pin.getByRole('combobox', { name: 'Download quality' }).selectOption('original');
    await pin.getByRole('button', { name: 'Pin offline', exact: true }).click();
    await expect(pin.locator('[data-status]')).toContainText('Pinned · 3 of 3', { timeout: 60_000 });
    await pin.getByRole('button', { name: 'Close', exact: true }).click();

    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole('button', { name: 'Toggle theme', exact: true }).click();
    await openPin(page, title);
    const narrow = e.path('m47-light-narrow-pinned.png');
    await page.screenshot({ path: narrow, fullPage: true, animations: 'disabled' }); artifacts.push(narrow);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await pin.getByRole('button', { name: 'Close', exact: true }).click();
    await page.setViewportSize({ width: 1280, height: 800 });

    await page.getByRole('button', { name: 'Log out', exact: true }).click();
    await page.waitForURL(url => url.pathname === '/login.html');
    await keyboardLogin(page, f.admin);
    await page.getByRole('button', { name: 'Settings', exact: true }).click();
    const saved = page.locator('#settings-modal [data-offline-library]');
    await expect(saved.locator('[data-status]')).toContainText('No saved collections.');
    const other = e.path('m47-other-account-empty.png');
    await page.screenshot({ path: other, fullPage: true }); artifacts.push(other);
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Log out', exact: true }).click();
    await page.waitForURL(url => url.pathname === '/login.html');
    await keyboardLogin(page, f.user);
    await page.goto(`/?music=album&id=${encodeURIComponent(album.album_key)}`);
    await expect(page.locator('#media-grid [data-collection-pin]')).toHaveText(/Pinned 3\/3 · Manage…/);
    await page.locator('#media-grid').getByRole('button', { name: 'More', exact: true }).click();
    await expect(page.getByRole('menu', { name: 'More collection actions' }).getByRole('button', { name: /Pinned 3\/3 · Manage…/ })).toBeVisible();
    await page.keyboard.press('Escape');

    await openPin(page, title);
    expect((await admin.delete('/api/v1/admin/permissions', {
      data: { set_id: f.setIds.Music, user_id: userId },
    })).ok()).toBe(true);
    revoked = true;
    const denied = page.waitForResponse(response => response.request().method() === 'HEAD'
      && response.url().includes('/api/media/') && response.url().includes('/download') && response.status() === 403);
    await pin.getByRole('button', { name: 'Play downloaded', exact: true }).click();
    await denied;
    await expect.poll(() => page.locator('#media-audio').evaluate((audio: HTMLAudioElement) => audio.paused)).toBe(true);
    const deniedShot = e.path('m47-denied-saved-playback.png');
    await page.screenshot({ path: deniedShot, fullPage: true }); artifacts.push(deniedShot);
    expect((await page.request.get(`/api/v1/music/albums/${album.album_key}`)).status()).toBe(404);
    expect((await admin.post('/api/v1/admin/permissions', {
      data: { set_id: f.setIds.Music, user_id: userId, role: 'viewer' },
    })).ok()).toBe(true);
    revoked = false;
    await page.goto(`/?music=album&id=${encodeURIComponent(album.album_key)}`);
    await openPin(page, title);
    await expect(pin.locator('[data-status]')).toContainText('Retry pinning');
    await pin.getByRole('button', { name: 'Retry pinning', exact: true }).click();
    await expect(pin.locator('[data-status]')).toContainText('Pinned · 3 of 3', { timeout: 60_000 });
    e.record({ feature: 'M47', scenario: 'narrow light UI, per-account saved collection isolation, actual ACL 403 blocks saved playback and restored access retries', result: 'pass',
      data: { account: f.user.username, denied_status: 403, restored: true }, artifacts });
  } catch (error) {
    e.record({ feature: 'M47', scenario: 'account and ACL boundary', result: 'fail', detail: String(error), artifacts });
    throw error;
  } finally {
    if (revoked) {
      const users = await (await admin.get('/api/v1/admin/users')).json();
      const userId = users.find((row: any) => row.username === f.user.username)?.id;
      if (userId) await admin.post('/api/v1/admin/permissions', { data: { set_id: f.setIds.Music, user_id: userId, role: 'viewer' } });
    }
    await admin.dispose();
  }
});
