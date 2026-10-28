/** M48: real browser policy, reconciliation, and offline playback. */
import { test, expect, type Page } from '@playwright/test';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, expectPlaying, captureOutput, assertValidCapture, mainSegment, openCollectionMoreAction } from './helpers/music-ui';

test.skip(process.env.PLAYER_M48_ISOLATED !== '1', 'Requires an isolated disposable music fixture');
const attempt = `${Date.now()}-${process.pid}`;
let fixture: MusicFixture, evidence: Evidence;

test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  if (!existsSync(join(fixture.dir, '.player-music-fixture')) || new URL(fixture.origin).hostname !== '127.0.0.1')
    throw Error('M48 needs a disposable localhost fixture');
  evidence = new Evidence(join(process.env.PLAYER_M48_EVIDENCE!, attempt), info.project.name, {
    browser_version: browser.version(), channel: info.project.use.channel,
    server_version: fixture.serverVersion, server_build: fixture.buildRevision, origin: fixture.origin,
  });
});

async function saved(page: Page) {
  return page.evaluate(async () => {
    const key = Object.keys(localStorage).find(name => /^player-offline-v1-\d+:index$/.test(name));
    if (!key) return { collections: {}, songs: [], available: [] as number[] };
    const index = JSON.parse(localStorage.getItem(key)!);
    const cache = await caches.open(key.slice(0, -6));
    const songs = Object.values(index.songs || {}) as any[];
    const available: number[] = [];
    for (const song of songs) if (await cache.match(song.url)) available.push(song.media.id);
    return { collections: index.collections, songs, available };
  });
}

async function automaticPanel(page: Page) {
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  const settings = page.locator('#settings-modal');
  const panel = settings.locator('[data-automatic-music]');
  await panel.locator('summary').click();
  await expect(panel).toHaveAttribute('open', '');
  return { settings, panel };
}

test('M48 updates a pinned playlist and starred songs, waits for Wi-Fi, then plays a retained copy offline', async ({ page, context }) => {
  test.setTimeout(240_000);
  let playlistId = '';
  let offline = false;
  let favoriteId = 0;
  const artifacts: string[] = [];
  try {
    await page.addInitScript(installAudioTap);
    await keyboardLogin(page, fixture.user);
    const first = fixture.mediaIds['Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3'];
    const second = fixture.mediaIds['Music/Aurora Lane - Night Transit (2021)/02 - Long Road Home.mp3'];
    const third = fixture.mediaIds['Music/Aurora Lane - Night Transit (2021)/03 - Harbour.flac'];
    favoriteId = third;
    const favorites = await page.request.get('/api/v1/media?favorites=true&type=audio&limit=200&offset=0');
    expect(favorites.ok()).toBe(true);
    if ((await favorites.json()).some((row: any) => row.id === third))
      expect((await page.request.post(`/api/v1/media/${third}/favorite`)).ok()).toBe(true);
    const name = `M48 ${attempt} ${test.info().project.name}`;
    const created = await page.request.post('/api/v1/playlists', { data: { name } });
    expect(created.ok()).toBe(true);
    playlistId = (await created.json()).playlist_id;
    expect((await page.request.post(`/api/v1/playlists/${playlistId}/songs`, {
      data: { action: 'add', media_ids: [first] },
    })).ok()).toBe(true);

    await page.goto(`/?music=playlist&id=${encodeURIComponent(playlistId)}`);
    await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
    await openCollectionMoreAction(page, /Pin offline|Manage…/);
    const dialog = page.getByRole('dialog', { name, exact: true });
    await dialog.getByRole('combobox', { name: 'Download quality' }).selectOption('original');
    await dialog.getByRole('button', { name: 'Pin offline', exact: true }).click();
    await expect(dialog.locator('[data-status]')).toContainText('Pinned · 1 of 1', { timeout: 60_000 });
    await dialog.getByRole('button', { name: 'Close', exact: true }).click();

    const { settings, panel } = await automaticPanel(page);
    await panel.locator('[data-playlists]').check();
    await panel.locator('[data-starred]').check();
    await panel.locator('[data-enabled]').check();
    await panel.locator('[data-sync]').click();
    await expect(panel.locator('[data-status]')).toContainText('waits for Wi-Fi', { timeout: 30_000 });
    expect((await saved(page)).available).toContain(first);
    await settings.locator('[data-music-network] [data-mode]').selectOption('wifi');
    await panel.locator('[data-sync]').click();
    await expect(panel.locator('[data-status]')).toContainText('Offline music is current', { timeout: 60_000 });

    // Change the source on the disposable server. The real Settings action
    // must fetch the new playlist/favorite state and save actual media bytes.
    expect((await page.request.post(`/api/v1/playlists/${playlistId}/songs`, {
      data: { action: 'add', media_ids: [second] },
    })).ok()).toBe(true);
    expect((await page.request.post(`/api/v1/media/${third}/favorite`)).ok()).toBe(true);
    await panel.locator('[data-sync]').click();
    await expect.poll(async () => (await saved(page)).available.sort((a, b) => a - b)).toEqual([first, second, third].sort((a, b) => a - b));
    const rows = await saved(page);
    expect(Object.values(rows.collections).some((row: any) => row.collection?.key === playlistId || row.title === name)).toBe(true);
    expect(rows.songs.filter((row: any) => row.automatic).length).toBe(2);
    await expect(panel.locator('[data-status]')).toContainText('3 current songs');

    // Make the selected interval due in this isolated tab, change the source,
    // then use a real browser offline/reconnect event. The open-tab scheduler
    // must fetch the new song without another Sync now click.
    const fourth = fixture.mediaIds['Music/Gain Check/Quiet.flac'];
    expect((await page.request.post(`/api/v1/playlists/${playlistId}/songs`, {
      data: { action: 'add', media_ids: [fourth] },
    })).ok()).toBe(true);
    await page.evaluate(() => {
      const key = Object.keys(localStorage).find(name => name.startsWith(`player-automatic-music-v1:${location.origin}:`));
      if (!key) throw Error('Automatic policy was not persisted');
      const current = JSON.parse(localStorage.getItem(key)!);
      localStorage.setItem(key, JSON.stringify({ ...current, nextDue: Date.now() - 1 }));
    });
    await context.setOffline(true);
    await expect.poll(() => page.evaluate(() => navigator.onLine)).toBe(false);
    await context.setOffline(false);
    await expect.poll(() => page.evaluate(() => navigator.onLine)).toBe(true);
    await expect(panel.locator('[data-status]')).toContainText('4 current songs', { timeout: 45_000 });
    expect((await saved(page)).available).toContain(fourth);
    const changed = await page.request.get(`/api/v1/playlists/${playlistId}`);
    expect(changed.ok()).toBe(true);
    const entry = (await changed.json()).entries.find((row: any) => row.media_id === fourth)?.entry_id;
    expect(entry).toBeTruthy();
    expect((await page.request.post(`/api/v1/playlists/${playlistId}/songs`, {
      data: { action: 'remove', entry_id: entry },
    })).ok()).toBe(true);
    await panel.locator('[data-sync]').click();
    await expect(panel.locator('[data-status]')).toContainText('3 current songs', { timeout: 45_000 });
    await expect(panel.locator('[data-status]')).toContainText('1 retired');

    const shot = evidence.path('automatic-narrow-dark.png');
    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({ path: shot, fullPage: true }); artifacts.push(shot);
    await settings.getByLabel('Theme').selectOption('light');
    const light = evidence.path('automatic-narrow-light.png');
    await page.screenshot({ path: light, fullPage: true }); artifacts.push(light);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.keyboard.press('Escape');
    await page.reload();
    const again = await automaticPanel(page);
    await expect(again.panel.locator('[data-enabled]')).toBeChecked();
    await expect(again.panel.locator('[data-status]')).toContainText('3 current songs');
    await page.keyboard.press('Escape');

    await expect.poll(() => page.evaluate(() => Boolean(navigator.serviceWorker.controller)), { timeout: 20_000 }).toBe(true);
    await context.setOffline(true); offline = true;
    await page.goto('/?offline=music');
    await expect(page.getByRole('heading', { name: 'Offline music' })).toBeVisible();
    await page.getByLabel('Browse').selectOption('Playlists');
    await page.getByRole('button', { name, exact: true }).click();
    await expect(page.locator('[data-results]')).toContainText('2 of 2 songs available');
    await page.getByRole('button', { name: 'Play now', exact: true }).click();
    await expectPlaying(page, first);
    const output = await captureOutput(page, 1500, evidence.path('automatic-offline-playback.wav'));
    assertValidCapture(output);
    expect(Math.abs((mainSegment(output.analysis)?.tone_hz || 0) - 440)).toBeLessThan(6);
    artifacts.push(output.wav);
    evidence.record({ feature: 'M48', scenario: 'automatic playlist/starred reconciliation, Wi-Fi policy, reload and offline audio', result: 'pass',
      data: { playlistId, first, second, third, fourth, automaticCopies: 3 }, artifacts });
  } catch (error) {
    evidence.record({ feature: 'M48', scenario: 'automatic playlist/starred reconciliation, Wi-Fi policy, reload and offline audio', result: 'fail', detail: String(error), artifacts });
    throw error;
  } finally {
    if (offline) await context.setOffline(false);
    if (favoriteId) {
      const favorites = await page.request.get('/api/v1/media?favorites=true&type=audio&limit=200&offset=0').catch(() => null);
      if (favorites?.ok() && (await favorites.json()).some((row: any) => row.id === favoriteId))
        await page.request.post(`/api/v1/media/${favoriteId}/favorite`).catch(() => {});
    }
    if (playlistId) await page.request.delete(`/api/v1/playlists/${playlistId}`).catch(() => {});
  }
});
