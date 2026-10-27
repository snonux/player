import { expect, request, test, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { keyboardLogin, openSidebarMusic } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires disposable local music fixture');
let fixture: MusicFixture;
let evidence: Evidence;
test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  evidence = new Evidence(fixture.evidenceDir, info.project.name, {
    browser_version: browser.version(), server_version: fixture.serverVersion,
    server_build: fixture.buildRevision, checkout: fixture.checkout,
  });
});

async function catalog(page: Page, name: 'Albums' | 'Artists' | 'Playlists') {
  await openSidebarMusic(page, name);
  if (name === 'Albums') await expect(page.locator('#result-count')).toContainText('Music ·');
  else await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
}

async function heart(page: Page, kind: string, key: string) {
  return page.locator(`[data-entity-favorite][data-kind="${kind}"][data-key="${key}"]`);
}

test('M34 entity hearts are scoped, durable and independent of file hearts', async ({ page, browser, context }) => {
  const artifacts: string[] = [];
  const admin = await request.newContext({ baseURL: fixture.origin });
  try {
    await keyboardLogin(page, fixture.user);
    expect((await admin.post('/api/v1/auth/login', { data: fixture.admin })).ok()).toBe(true);
    const album = (await (await page.request.get('/api/v1/music/albums')).json())
      .find((item: any) => item.title === 'Night Transit');
    const artist = (await (await page.request.get('/api/v1/music/artists')).json())
      .find((item: any) => item.name === 'Aurora Lane');
    expect(album?.album_key).toBeTruthy();
    expect(artist?.artist_key).toBeTruthy();
    // Projects share one disposable database; establish the same baseline in each browser.
    expect((await page.request.delete(`/api/v1/music/favorites/album/${encodeURIComponent(album.album_key)}`)).ok()).toBe(true);
    expect((await page.request.delete(`/api/v1/music/favorites/artist/${encodeURIComponent(artist.artist_key)}`)).ok()).toBe(true);
    for (const item of await (await page.request.get('/api/v1/playlists?favorite=true')).json())
      expect((await page.request.delete(`/api/v1/music/favorites/playlist/${item.playlist_id}`)).ok()).toBe(true);
    const mediaId = fixture.mediaIds['Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3'];
    // Existing file/song hearts use their own endpoint and must survive all entity changes.
    const fileFavorites = async () => (await (await page.request.get('/api/v1/media?favorites=true&limit=1000')).json());
    if (!(await fileFavorites()).some((item: any) => item.id === mediaId))
      expect((await page.request.post(`/api/v1/media/${mediaId}/favorite`)).ok()).toBe(true);
    expect((await fileFavorites()).some((item: any) => item.id === mediaId)).toBe(true);

    await catalog(page, 'Albums');
    const albumCard = page.locator(`[data-album="${album.album_key}"]`);
    const albumHeart = await heart(page, 'album', album.album_key);
    await albumHeart.click();
    await expect(albumHeart).toHaveAttribute('aria-pressed', 'true');
    await expect(albumHeart).toBeFocused();
    await page.getByRole('checkbox', { name: 'Favorites only' }).check();
    await expect(albumCard).toBeVisible();
    await expect(page.locator('[data-album]')).toHaveCount(1);
    await albumCard.focus();
    await page.keyboard.press('Enter');
    await expect(page.getByRole('heading', { name: 'Night Transit', exact: true })).toBeVisible();
    await expect(await heart(page, 'album', album.album_key)).toHaveAttribute('aria-pressed', 'true');
    await page.keyboard.press('Backspace');
    await expect(albumCard.locator('.collection-open')).toBeFocused();
    await expect(page.getByRole('checkbox', { name: 'Favorites only' })).toBeChecked();

    await catalog(page, 'Artists');
    const artistHeart = await heart(page, 'artist', artist.artist_key);
    await artistHeart.click();
    await expect(artistHeart).toHaveAttribute('aria-pressed', 'true');
    await page.getByRole('checkbox', { name: 'Favorites only' }).check();
    await expect(page.locator(`[data-artist-card="${artist.artist_key}"]`)).toBeVisible();
    await expect(page.locator('[data-artist-card]')).toHaveCount(1);
    await page.getByRole('button', { name: 'Open artist Aurora Lane' }).click();
    await expect(await heart(page, 'artist', artist.artist_key)).toHaveAttribute('aria-pressed', 'true');

    await catalog(page, 'Playlists');
    await page.getByRole('button', { name: 'Create playlist', exact: true }).click();
    const name = `M34 Favorites ${Date.now()}`;
    await page.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
    await page.getByRole('textbox', { name: 'Name', exact: true }).press('r');
    await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toHaveValue(name + 'r');
    await page.getByRole('combobox', { name: 'Visibility' }).selectOption('shared');
    await page.getByRole('button', { name: 'Create playlist', exact: true }).last().click();
    await expect(page.getByRole('heading', { name: name + 'r', exact: true })).toBeVisible();
    const playlistId = new URL(page.url()).searchParams.get('id')!;
    await expect(await heart(page, 'playlist', playlistId)).toHaveAttribute('aria-pressed', 'false');
    await (await heart(page, 'playlist', playlistId)).click();
    await expect(await heart(page, 'playlist', playlistId)).toHaveAttribute('aria-pressed', 'true');
    await page.keyboard.press('Backspace');
    await page.getByRole('checkbox', { name: 'Favorites only' }).check();
    await expect(page.locator(`[data-playlist="${playlistId}"]`)).toBeVisible();
    await expect(page.locator('[data-playlist]')).toHaveCount(1);

    // A second signed-in client sees the shared playlist but has its own heart state.
    const otherContext = await browser.newContext();
    try {
      const otherPage = await otherContext.newPage();
      await keyboardLogin(otherPage, fixture.admin);
      await catalog(otherPage, 'Playlists');
      await expect(otherPage.locator(`[data-playlist="${playlistId}"]`)).toBeVisible();
      await expect(await heart(otherPage, 'playlist', playlistId)).toHaveAttribute('aria-pressed', 'false');
      await (await heart(otherPage, 'playlist', playlistId)).click();
      await expect(await heart(otherPage, 'playlist', playlistId)).toHaveAttribute('aria-pressed', 'true');
      await otherPage.getByRole('checkbox', { name: 'Favorites only' }).check();
      await expect(otherPage.locator(`[data-playlist="${playlistId}"]`)).toBeVisible();
      expect((await (await admin.get('/api/v1/music/albums?favorite=true')).json()).some((item: any) => item.album_key === album.album_key)).toBe(false);
      expect((await (await admin.get('/api/v1/music/artists?favorite=true')).json()).some((item: any) => item.artist_key === artist.artist_key)).toBe(false);
    } finally { await otherContext.close(); }

    // A failed write preserves the old pressed state and can be retried online.
    await catalog(page, 'Albums');
    await page.getByRole('checkbox', { name: 'Favorites only' }).check();
    await context.setOffline(true);
    await (await heart(page, 'album', album.album_key)).click();
    await expect(await heart(page, 'album', album.album_key)).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#toast')).toHaveClass(/error/);
    await context.setOffline(false);
    await (await heart(page, 'album', album.album_key)).click();
    await expect(page.getByText('No favorite albums. Show all albums to favorite one.')).toBeVisible();
    await expect(page.getByRole('checkbox', { name: 'Favorites only' })).toBeChecked();
    await page.getByRole('checkbox', { name: 'Favorites only' }).uncheck();
    await expect(await heart(page, 'album', album.album_key)).toHaveAttribute('aria-pressed', 'false');
    await (await heart(page, 'album', album.album_key)).click();

    for (const theme of ['dark', 'light']) {
      await page.setViewportSize({ width: 390, height: 844 });
      if (await page.locator('html').getAttribute('data-theme') !== theme)
        await page.getByRole('button', { name: 'Toggle theme' }).click();
      await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      const shot = evidence.path(`m34-${theme}-narrow.png`);
      await page.screenshot({ path: shot, fullPage: true, animations: 'disabled' });
      artifacts.push(shot);
    }

    // Reload and a real server process restart retain account-scoped favorites.
    await page.reload();
    expect((await (await page.request.get('/api/v1/music/albums?favorite=true')).json()).some((item: any) => item.album_key === album.album_key)).toBe(true);
    const script = resolve(__dirname, '../../../../player-android/integration_test/support/music_fixture.py');
    execFileSync('python3', [script, 'restart', fixture.dir], { timeout: 30_000 });
    await page.reload();
    await expect(await heart(page, 'album', album.album_key)).toHaveAttribute('aria-pressed', 'true');
    expect((await fileFavorites()).some((item: any) => item.id === mediaId)).toBe(true);
    expect((await (await page.request.get('/api/v1/music/artists?favorite=true')).json()).some((item: any) => item.artist_key === artist.artist_key)).toBe(true);
    expect((await (await page.request.get('/api/v1/playlists?favorite=true')).json()).some((item: any) => item.playlist_id === playlistId)).toBe(true);

    // A hidden album/artist and a private playlist never appear in the viewer's collection.
    const hiddenAlbum = (await (await admin.get('/api/v1/music/albums')).json()).find((item: any) => item.title === 'Hidden Album');
    const hiddenArtist = (await (await admin.get('/api/v1/music/artists')).json()).find((item: any) => item.name === 'Secret Artist');
    expect(hiddenAlbum?.album_key).toBeTruthy();
    expect(hiddenArtist?.artist_key).toBeTruthy();
    expect((await page.request.put(`/api/v1/music/favorites/album/${encodeURIComponent(hiddenAlbum.album_key)}`)).status()).toBe(404);
    expect((await page.request.put(`/api/v1/music/favorites/artist/${encodeURIComponent(hiddenArtist.artist_key)}`)).status()).toBe(404);
    const privatePlaylist = await admin.post('/api/v1/playlists', { data: { name: 'M34 private', comment: '', visibility: 'private' } });
    expect(privatePlaylist.ok()).toBe(true);
    const privateId = (await privatePlaylist.json()).playlist_id;
    expect((await page.request.put(`/api/v1/music/favorites/playlist/${privateId}`)).status()).toBe(404);
    expect((await (await page.request.get('/api/v1/playlists?favorite=true')).json()).some((item: any) => item.playlist_id === privateId)).toBe(false);
    evi('pass', { album_key: album.album_key, artist_key: artist.artist_key, playlist_id: playlistId, media_id: mediaId }, artifacts);
  } catch (error) {
    evi('fail', { error: String(error) }, artifacts);
    throw error;
  } finally { await admin.dispose(); }
});

function evi(result: 'pass' | 'fail', data: Record<string, unknown>, artifacts: string[]) {
  evidence.record({ feature: 'M34', scenario: 'album/artist/playlist hearts, collections, account scope, ACL, offline retry, restart, file-heart independence and narrow themes', result, data, artifacts });
}
