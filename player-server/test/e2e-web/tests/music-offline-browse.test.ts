/** M49 local-only music browsing in installed Chrome and Firefox. */
import { test, expect, request, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, openFolder, expectPlaying, captureOutput, assertValidCapture, mainSegment, openCollectionMoreAction } from './helpers/music-ui';

test.skip(process.env.PLAYER_M49_ISOLATED !== '1', 'Requires an exclusively owned disposable music fixture');
const attempt = `${Date.now()}-${process.pid}`;
let fixture: MusicFixture, evidence: Evidence;

test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  if (!existsSync(join(fixture.dir, '.player-music-fixture')) || new URL(fixture.origin).hostname !== '127.0.0.1')
    throw Error('M49 requires a disposable localhost music fixture');
  evidence = new Evidence(join(resolve(process.env.PLAYER_M49_EVIDENCE || join(tmpdir(), 'player-m49-web-evidence')), attempt), info.project.name, {
    browser_version: browser.version(), channel: info.project.use.channel,
    server_version: fixture.serverVersion, server_build: fixture.buildRevision,
    checkout: execFileSync('git', ['rev-parse', 'HEAD']).toString().trim(), origin: fixture.origin,
  });
});

async function pin(page: Page, kind: 'album' | 'playlist', key: string, title: string, expected: number) {
  await page.goto(`/?music=${kind}&id=${encodeURIComponent(key)}`);
  await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible();
  await openCollectionMoreAction(page, /Pin offline|Manage…/);
  const dialog = page.getByRole('dialog', { name: title, exact: true });
  await dialog.getByRole('combobox', { name: 'Download quality' }).selectOption('original');
  await dialog.getByRole('button', { name: 'Pin offline', exact: true }).click();
  await expect(dialog.locator('[data-status]')).toContainText(`Pinned · ${expected} of ${expected}`, { timeout: 60_000 });
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
}

async function queue(page: Page) {
  return page.evaluate(async () => (await import('/js/queue.js' as string)).queueState());
}

async function openRow(page: Page, name: string, keyboard = false) {
  const row = page.locator('[data-results]').getByRole('button', { name, exact: true });
  await expect(row).toBeVisible();
  if (keyboard) { await row.focus(); await page.keyboard.press('Enter'); }
  else await row.click();
  await expect(page.locator('[data-results] h2')).toHaveText(name);
}

async function removeSavedByte(page: Page, accountId: number, mediaId: number) {
  return page.evaluate(async ({ accountId, mediaId }) => {
    const name = `player-offline-v1-${accountId}`;
    const index = JSON.parse(localStorage.getItem(`${name}:index`) || '{}');
    const song = Object.values(index.songs || {}).find((row: any) => row.media.id === mediaId) as any;
    return song ? (await caches.open(name)).delete(song.url) : false;
  }, { accountId, mediaId });
}

test('M49 empty local store explains how to get offline music', async ({ page }) => {
  await page.goto('/index.html?offline=music');
  await expect(page.getByRole('heading', { name: 'Offline music' })).toBeVisible();
  await expect(page.locator('[data-status]')).toHaveText('No downloaded music yet.');
  await expect(page.getByLabel('Saved server and account')).toBeDisabled();
  const shot = evidence.path('m49-empty.png');
  await page.screenshot({ path: shot });
  evidence.record({ feature: 'M49', scenario: 'empty local store and clear offline entry', result: 'pass',
    artifacts: [shot] });
});

test('M49 downloads and cached song browse, search, queue and play without network', async ({ page, context }) => {
  test.setTimeout(300_000);
  const artifacts: string[] = [];
  let playlistId = '';
  let offline = false;
  try {
    await page.addInitScript(installAudioTap);
    await keyboardLogin(page, fixture.user);
    const albums = await (await page.request.get('/api/v1/music/albums')).json();
    const night = albums.find((row: any) => row.title === 'Night Transit');
    const synth = albums.find((row: any) => row.title === 'Synth Compilation');
    expect(night && synth).toBeTruthy();
    const nightSongs = (await (await page.request.get(`/api/v1/music/albums/${night.album_key}`)).json()).songs;
    const synthSongs = (await (await page.request.get(`/api/v1/music/albums/${synth.album_key}`)).json()).songs;
    expect(nightSongs).toHaveLength(3);
    const playlistName = `M49 offline mix ${attempt}`;
    const created = await page.request.post('/api/v1/playlists', { data: { name: playlistName } });
    expect(created.ok()).toBe(true);
    playlistId = (await created.json()).playlist_id;
    expect((await page.request.post(`/api/v1/playlists/${playlistId}/songs`, {
      data: { action: 'add', media_ids: [nightSongs[0].id, synthSongs[0].id, nightSongs[0].id] },
    })).ok()).toBe(true);
    await pin(page, 'album', night.album_key, 'Night Transit', 3);
    await pin(page, 'playlist', playlistId, playlistName, 2);

    // Ordinary listening creates a disposable cached song outside both pins.
    await page.getByRole('button', { name: 'Settings', exact: true }).click();
    const settings = page.locator('#settings-modal');
    await settings.getByLabel('Playback cache budget').selectOption('128');
    await settings.getByLabel('Playback buffering').selectOption('complete');
    await page.keyboard.press('Escape');
    await openFolder(page, fixture.setIds.Music, 'Gain Check');
    const cached = page.locator('.media-card').filter({ has: page.getByRole('button', { name: 'No Gain Tags', exact: true }) });
    await cached.hover(); await cached.getByRole('button', { name: 'Play', exact: true }).click();
    const cachedId = fixture.mediaIds['Music/Gain Check/No Gain Tags.mp3'];
    await expectPlaying(page, cachedId);
    await expect.poll(() => page.evaluate(id => {
      const key = Object.keys(localStorage).find(value => /^player-playback-v1-\d+:index$/.test(value));
      return key && Object.values(JSON.parse(localStorage.getItem(key) || '{}')).some((row: any) => row.media.id === id);
    }, cachedId), { timeout: 60_000 }).toBe(true);

    // A second real account gets a different saved album on this browser.
    await page.getByRole('button', { name: 'Log out', exact: true }).click();
    await page.waitForURL(url => url.pathname === '/login.html', { waitUntil: 'domcontentloaded' });
    await keyboardLogin(page, fixture.admin);
    const users = await (await page.request.get('/api/v1/admin/users')).json();
    const userId = users.find((row: any) => row.username === fixture.user.username).id;
    const adminId = users.find((row: any) => row.username === fixture.admin.username).id;
    await pin(page, 'album', synth.album_key, 'Synth Compilation', synthSongs.length);
    await expect.poll(() => page.evaluate(() => Boolean(navigator.serviceWorker.controller)), { timeout: 20_000 }).toBe(true);
    await page.getByRole('button', { name: 'Settings', exact: true }).click();
    await expect(settings.getByRole('link', { name: 'Browse offline music' })).toBeVisible();
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Log out', exact: true }).click();
    await page.waitForURL(url => url.pathname === '/login.html', { waitUntil: 'domcontentloaded' });
    await expect(page.getByRole('link', { name: 'Browse offline music' })).toBeVisible();

    await context.setOffline(true); offline = true;
    await page.getByRole('link', { name: 'Browse offline music' }).click();
    await expect(page.getByRole('heading', { name: 'Offline music' })).toBeVisible();
    await page.reload(); // Cold open from the service worker, still disconnected.
    await expect(page.getByRole('heading', { name: 'Offline music' })).toBeVisible();
    const requests: string[] = [];
    page.on('request', req => { if (new URL(req.url()).pathname.startsWith('/api/')) requests.push(req.url()); });
    const account = page.getByLabel('Saved server and account');
    await account.selectOption(String(userId));
    await expect(page.locator('[data-status]')).toContainText('Saved on this device.');
    await expect(page.getByRole('button', { name: 'First Light', exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'No Gain Tags', exact: true })).toBeVisible();
    const search = page.getByLabel('Search downloaded music');
    await page.keyboard.press('/'); await expect(search).toBeFocused();
    await search.fill('j/k'); await expect(search).toHaveValue('j/k');
    await expect(page.locator('[data-status]')).toHaveText('No downloaded music matches.');
    await search.fill('First Light');
    await expect(page.getByRole('button', { name: 'First Light', exact: true })).toBeVisible();
    await expect(page.locator('[data-status]')).toContainText('Saved on this device.');
    await search.fill(''); await search.blur();

    await page.getByLabel('Browse').selectOption('Albums');
    await openRow(page, 'Night Transit', true);
    await expect(page.locator('[data-results]')).toContainText('3 of 3 songs available');
    await expect(page.locator('[data-results]')).toContainText('Downloaded');
    await page.keyboard.press('Escape');
    await expect(page.getByRole('button', { name: 'Night Transit', exact: true })).toBeFocused();
    const dark = evidence.path('m49-albums-dark.png'); await page.screenshot({ path: dark, fullPage: true }); artifacts.push(dark);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole('button', { name: 'Toggle theme', exact: true }).click();
    await page.getByLabel('Browse').selectOption('Artists');
    await openRow(page, 'Aurora Lane');
    await expect(page.locator('[data-results]')).toContainText('2 of 2 songs available');
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    const light = evidence.path('m49-artists-light-narrow.png'); await page.screenshot({ path: light, fullPage: true }); artifacts.push(light);
    await page.getByRole('button', { name: 'Back', exact: true }).click();
    await openRow(page, 'Aurora Lane feat. Kei');
    await expect(page.locator('[data-results]')).toContainText('1 of 1 songs available');
    await page.getByRole('button', { name: 'Back', exact: true }).click();
    await page.getByLabel('Browse').selectOption('Playlists');
    await openRow(page, playlistName);
    await expect(page.locator('[data-results]')).toContainText('2 of 2 songs available');
    await page.getByRole('button', { name: 'Back', exact: true }).click();

    // Remove only one saved byte: the playlist stays browsable and queues only
    // its available member, with explicit reconnection guidance.
    expect(await removeSavedByte(page, userId, synthSongs[0].id)).toBe(true);
    await page.getByRole('button', { name: 'Refresh saved collections' }).click();
    await openRow(page, playlistName);
    await expect(page.locator('[data-results]')).toContainText('1 of 2 songs available');
    await expect(page.locator('[data-results]')).toContainText('Some downloads are missing.');
    await page.getByRole('button', { name: 'Add to queue', exact: true }).click();
    await expect.poll(async () => (await queue(page)).entries.length).toBe(1);
    expect((await queue(page)).active).toBe(false);
    const partial = evidence.path('m49-partial-playlist.png'); await page.screenshot({ path: partial, fullPage: true }); artifacts.push(partial);
    await page.getByRole('button', { name: 'Back', exact: true }).click();
    await page.getByLabel('Browse').selectOption('Albums');
    await openRow(page, 'Night Transit');
    await page.getByRole('button', { name: 'Play now', exact: true }).click();
    await expectPlaying(page, nightSongs[0].id);
    const state = await queue(page);
    expect(state.entries).toHaveLength(3);
    expect((await page.locator('#media-audio').evaluate((audio: HTMLAudioElement) => audio.currentSrc)).startsWith('blob:')).toBe(true);
    const sound = await captureOutput(page, 1500, evidence.path('m49-offline-album.wav'));
    assertValidCapture(sound);
    expect(Math.abs((mainSegment(sound.analysis)?.tone_hz || 0) - 440)).toBeLessThan(6);
    artifacts.push(sound.wav);
    await page.keyboard.press('Q');
    const panel = page.locator('#queue-panel');
    await expect(panel).toHaveClass(/open/);
    await panel.getByRole('button', { name: 'More', exact: true }).click();
    const moreMenu = page.getByRole('menu', { name: 'More queue actions', exact: true });
    await expect(moreMenu).toBeVisible();
    await expect(moreMenu.getByRole('button', { name: 'Continue session' })).toHaveCount(0);
    await expect(moreMenu.getByRole('button', { name: 'Save as playlist' })).toHaveCount(0);
    await expect(moreMenu.getByRole('button', { name: 'Clear queue' })).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(panel.getByRole('button', { name: 'Autoplay related music' })).toBeDisabled();
    await page.keyboard.press('Escape');

    await account.selectOption(String(adminId));
    await expect.poll(async () => (await queue(page)).entries.length).toBe(0);
    await expect(page.getByRole('button', { name: 'Synth Compilation', exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Night Transit', exact: true })).toHaveCount(0);
    await account.selectOption(String(userId));
    await expect(page.getByRole('button', { name: 'Night Transit', exact: true })).toBeVisible();
    expect((await queue(page)).entries).toHaveLength(0);

    await openRow(page, 'Night Transit');
    await page.getByRole('button', { name: 'Play now', exact: true }).click();
    await expectPlaying(page, nightSongs[0].id);
    await context.setOffline(false); offline = false;
    expect((await queue(page)).entries).toHaveLength(3);
    await expect(page.getByRole('heading', { name: 'Offline music' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Sign In' })).toBeVisible();
    expect(requests).toEqual([]);
    const offlineAPIRequests = requests.length;
    const restored = evidence.path('m49-reconnected-still-local.png'); await page.screenshot({ path: restored, fullPage: true }); artifacts.push(restored);
    await page.getByRole('link', { name: 'Sign In' }).click();
    await keyboardLogin(page, fixture.user);
    await page.getByRole('button', { name: 'Settings', exact: true }).click();
    await expect(page.locator('#settings-modal [data-offline-library]')).toContainText('Night Transit');
    await expect(page.locator('#settings-modal [data-offline-library]')).toContainText(playlistName);
    evidence.record({ feature: 'M49', scenario: 'real album/playlist pins and cached song, cold offline browse/search, partial membership, queue/audio, account isolation and explicit reconnect', result: 'pass',
      data: { user_id: userId, admin_id: adminId, album_ids: nightSongs.map((song: any) => song.id), cached_id: cachedId,
        partial_playlist_count: 1, offline_audio: mainSegment(sound.analysis), offline_api_requests: offlineAPIRequests }, artifacts });
  } catch (error) {
    evidence.record({ feature: 'M49', scenario: 'offline music browser journey', result: 'fail', detail: String(error), artifacts });
    throw error;
  } finally {
    if (offline) await context.setOffline(false);
    if (playlistId) {
      const cleanup = await request.newContext({ baseURL: fixture.origin });
      try {
        if ((await cleanup.post('/api/v1/auth/login', { data: fixture.user })).ok())
          await cleanup.delete(`/api/v1/playlists/${playlistId}`);
      } finally { await cleanup.dispose(); }
    }
  }
});
