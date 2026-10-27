/** M42: installed-browser song, album and artist mix journeys. */
import { test, expect, type Page } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, openFolder, selectByKeys, expectPlaying, captureOutput, assertValidCapture, mainSegment, resetProgress } from './helpers/music-ui';

test.skip(!process.env.PLAYER_M42_ISOLATED, 'Requires disposable M42 fixture');
test.use({ serviceWorkers: 'block' });
let f: MusicFixture, e: Evidence;
const mix = (page: Page, kind: 'artist' | 'album' | 'song') => page.locator(kind === 'artist' ? 'section[aria-label="Artist radio"]' : 'section[aria-label="Instant Mix"]');
async function queue(page: Page) {
  return page.evaluate(async () => {
    const q = (await import('/js/queue.js' as string)).queueState();
    return { ids: q.entries.map((x: any) => x.media.id), label: q.label, loading: q.loading };
  });
}
async function apiMix(page: Page, kind: string, key: string, rating = 0) {
  const result = await page.request.get(`/api/v1/music/mix/${kind}/${encodeURIComponent(key)}?min_rating=${rating}`);
  expect(result.ok()).toBe(true);
  return result.json() as Promise<{title: string; songs: {id: number}[]}>;
}
async function start(page: Page, kind: 'artist' | 'album' | 'song', rating = 0) {
  const section = mix(page, kind);
  await expect(section).toBeVisible();
  await section.getByLabel('Minimum song rating').selectOption(String(rating));
  const button = section.getByRole('button', { name: kind === 'artist' ? 'Artist radio' : 'Start mix' });
  await button.focus();
  await page.keyboard.press('Enter');
  return section;
}
async function openDetail(page: Page, kind: 'artist' | 'album', key: string) {
  await page.goto(`/?music=${kind}&id=${encodeURIComponent(key)}`);
  await expect(mix(page, kind)).toBeVisible();
}

test.beforeAll(async ({ browser }, info) => {
  f = loadFixture();
  e = new Evidence(process.env.PLAYER_M42_EVIDENCE!, info.project.name,
    { feature: 'M42', browser_version: browser.version(), server_version: f.serverVersion, build_revision: f.buildRevision });
});

test('M42 source mixes, rating filter, no match and failure preserve queue', async ({ page }) => {
  test.setTimeout(150_000);
  const first = track(f, 'Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3');
  await page.addInitScript(installAudioTap);
  await keyboardLogin(page, f.user);
  expect((await page.request.put(`/api/v1/music/ratings/song/${first.id}`, { data: { rating: 0 } })).ok()).toBe(true);
  const albums = await (await page.request.get('/api/v1/music/albums')).json();
  const artists = await (await page.request.get('/api/v1/music/artists')).json();
  const album = albums.find((a: any) => a.title === 'Night Transit');
  const artist = artists.find((a: any) => a.name === 'Aurora Lane');
  expect(album && artist).toBeTruthy();
  await openDetail(page, 'album', album.album_key);
  const empty = await start(page, 'album', 5);
  await expect(empty.getByRole('status')).toContainText('No matching songs for Night Transit');
  expect((await queue(page)).ids).toEqual([]);

  expect((await page.request.put(`/api/v1/music/ratings/song/${first.id}`, { data: { rating: 5 } })).ok()).toBe(true);
  const rated = await apiMix(page, 'album', album.album_key, 5);
  expect(rated.songs.map(s => s.id)).toEqual([first.id]);
  await resetProgress(page, first.id);
  await start(page, 'album', 5);
  await expect(empty.getByRole('status')).toContainText('1 song');
  expect((await queue(page)).ids).toEqual([first.id]);
  await expectPlaying(page, first.id);
  const sound = await captureOutput(page, 1300, e.path('rated-album-440hz.wav'));
  assertValidCapture(sound);
  expect(Math.abs(mainSegment(sound.analysis)!.tone_hz! - 440)).toBeLessThan(5);
  await page.locator('#btn-play').click();

  const albumResult = await apiMix(page, 'album', album.album_key);
  await start(page, 'album');
  await expect(empty.getByRole('status')).toContainText(`${albumResult.songs.length} songs`);
  expect((await queue(page)).ids).toEqual(albumResult.songs.map(s => s.id));
  await page.locator('#btn-play').click();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: e.path('album-narrow-dark.png'), fullPage: true });
  await page.getByRole('button', { name: 'Toggle theme', exact: true }).click();
  await page.screenshot({ path: e.path('album-narrow-light.png'), fullPage: true });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);

  await openDetail(page, 'artist', artist.artist_key);
  const artistResult = await apiMix(page, 'artist', artist.artist_key);
  const radio = await start(page, 'artist');
  await expect(radio.getByRole('status')).toContainText(`Artist radio · Aurora Lane · ${artistResult.songs.length} songs`);
  expect((await queue(page)).ids).toEqual(artistResult.songs.map(s => s.id));
  await page.locator('#btn-play').click();

  await openFolder(page, f.setIds.Music, 'Aurora Lane - Night Transit (2021)');
  await selectByKeys(page, first.title);
  await page.keyboard.press('i');
  await expect(page.locator('#media-info-modal')).toHaveClass(/open/);
  const songResult = await apiMix(page, 'song', String(first.id));
  const song = await start(page, 'song');
  await expect(song.getByRole('status')).toContainText(`Mix · First Light · ${songResult.songs.length} songs`);
  expect((await queue(page)).ids).toEqual(songResult.songs.map(s => s.id));
  const before = await queue(page);
  const route = /\/api\/music\/mix\/song\//;
  await page.route(route, r => r.abort());
  await start(page, 'song');
  await expect(song.getByRole('status')).toContainText('Could not start mix.');
  expect((await queue(page)).ids).toEqual(before.ids);
  await page.unroute(route);
  await page.keyboard.press('Escape');
  await expect(page.locator('#media-info-modal')).not.toHaveClass(/open/);
  e.record({ feature: 'M42', scenario: 'album/song/artist mix actual queue, personal min rating, no match, offline error and real audio', result: 'pass',
    data: { album: albumResult.songs.map(s => s.id), artist: artistResult.songs.map(s => s.id), song: songResult.songs.map(s => s.id), rated: rated.songs.map(s => s.id) },
    artifacts: ['rated-album-440hz.wav', 'album-narrow-dark.png', 'album-narrow-light.png'] });
});
