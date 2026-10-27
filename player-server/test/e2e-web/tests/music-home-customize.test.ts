/** M39: installed-browser home preferences, pins and account boundaries. */
import { randomBytes } from 'node:crypto';
import { test, expect, type Page } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, openSidebarMusic, expectPlaying, captureOutput, assertValidCapture, mainSegment } from './helpers/music-ui';

test.skip(!process.env.PLAYER_M39_ISOLATED, 'Requires disposable M39 fixture');
test.use({ serviceWorkers: 'block' });
let f: MusicFixture, e: Evidence;
const panel = (p: Page) => p.locator('details[aria-label="Discover albums"]');
const editor = (p: Page) => panel(p).locator(':scope > details');
const names = (p: Page) => panel(p).locator('section.music-heading h3').allTextContents();

async function home(page: Page) {
  await openSidebarMusic(page, 'Albums');
  await expect(panel(page)).toBeVisible();
  if (!(await panel(page).evaluate(el => (el as HTMLDetailsElement).open)))
    await panel(page).locator(':scope > summary').click();
  await expect(panel(page)).toHaveAttribute('open', '');
}
async function customize(page: Page) {
  if (!(await editor(page).evaluate(el => (el as HTMLDetailsElement).open)))
    await editor(page).locator(':scope > summary').click();
  await expect(editor(page)).toHaveAttribute('open', '');
}

test.beforeAll(async ({ browser }, info) => {
  f = loadFixture();
  e = new Evidence(process.env.PLAYER_M39_EVIDENCE!, info.project.name,
    { feature: 'M39', browser_version: browser.version(), server_version: f.serverVersion, build_revision: f.buildRevision });
});

test('M39 customize, pin, recover and isolate home per account', async ({ page, context }, info) => {
  test.setTimeout(150_000);
  const actor = info.project.name === 'chrome' ? f.user : f.admin;
  const observer = info.project.name === 'chrome' ? f.admin : f.user;
  const first = track(f, 'Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3').id;
  const name = `M39 ${info.project.name} ${Date.now()}`;
  await page.addInitScript(installAudioTap);
  await keyboardLogin(page, actor);
  const response = await page.request.post('/api/v1/playlists', { data: { name, comment: '', visibility: 'private' } });
  expect(response.ok()).toBe(true);
  const id = (await response.json()).playlist_id as string;
  expect((await page.request.post(`/api/v1/playlists/${id}/songs`, { data: { action: 'add', media_ids: [first] } })).ok()).toBe(true);
  const albums = await (await page.request.get('/api/v1/music/albums')).json();
  const album = albums.find((x: any) => x.title === 'Night Transit');
  expect(album).toBeTruthy();
  expect((await page.request.put(`/api/v1/music/favorites/album/${encodeURIComponent(album.album_key)}`)).ok()).toBe(true);
  const event = randomBytes(16).toString('hex');
  const sync = await page.request.post(`/api/v1/media/${first}/playback-sync`,
    { data: { music_plays: [{ id: event, observed_ms: Date.now(), accumulated_seconds: 60 }] } });
  expect(sync.ok()).toBe(true);
  expect((await sync.json()).accepted_music_plays).toContain(event);

  await home(page);
  await expect(panel(page).getByRole('heading', { name: 'Liked-album mix' })).toBeVisible();
  await expect(panel(page).getByRole('heading', { name: 'Rediscover older plays' })).toBeVisible();
  await customize(page);
  const original = await names(page);
  expect(original[0]).toBe('Recently added');
  const move = editor(page).getByRole('button', { name: 'Move Recently added down' });
  await move.focus(); await page.keyboard.press('Enter');
  await expect(move).toBeFocused();
  expect((await names(page))[0]).toBe('Recently played');
  await editor(page).locator('[data-home-show="random"]').uncheck();
  expect(await names(page)).not.toContain('Random albums');
  const pin = editor(page).locator(`[data-home-pin="${id}"]`);
  await pin.check();
  await expect(panel(page).getByRole('button', { name, exact: true })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: e.path('home-narrow-dark.png') });
  await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'light'));
  await page.screenshot({ path: e.path('home-narrow-light.png') });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);

  await page.keyboard.press('Escape');
  await expect(editor(page)).not.toHaveAttribute('open', '');
  await expect(editor(page).locator(':scope > summary')).toBeFocused();
  const pinSection = panel(page).locator('section.music-heading').filter({ has: page.getByRole('heading', { name: 'Pinned playlists' }) });
  await pinSection.getByRole('button', { name: 'Play playlist' }).click();
  await expectPlaying(page, first);
  const capture = await captureOutput(page, 1200, e.path('pinned-playback.wav'));
  assertValidCapture(capture);
  expect(Math.abs(mainSegment(capture.analysis)!.tone_hz! - 440)).toBeLessThan(5);
  await page.locator('#btn-play').click();

  await page.reload();
  await home(page);
  expect((await names(page))[0]).toBe('Recently played');
  expect(await names(page)).not.toContain('Random albums');
  await expect(panel(page).getByRole('button', { name, exact: true })).toBeVisible();
  const scope = await page.evaluate(() => Object.keys(localStorage).find(k => k.startsWith('player.music-home.v1:')));
  expect(scope).toMatch(/^player\.music-home\.v1:/);

  const playlistList = /\/api\/playlists(?:\?|$)/;
  await page.route(playlistList, route => route.abort());
  await page.reload();
  await home(page);
  await customize(page);
  await expect(editor(page)).toContainText('Could not load playlists. Check your connection.');
  await expect(panel(page)).toContainText('Playlists unavailable. Retry in Customize home.');
  await page.unroute(playlistList);
  await editor(page).getByRole('button', { name: 'Retry playlists' }).click();
  await expect(editor(page).locator(`[data-home-pin="${id}"]`)).toBeChecked();
  await expect(panel(page).getByRole('button', { name, exact: true })).toBeVisible();

  await page.getByRole('button', { name: 'Log out', exact: true }).click();
  await page.waitForURL(url => url.pathname === '/login.html');
  await keyboardLogin(page, observer);
  await home(page);
  await customize(page);
  expect((await names(page))[0]).toBe('Recently added');
  expect(await names(page)).toContain('Random albums');
  await expect(panel(page).getByRole('button', { name, exact: true })).toHaveCount(0);
  if (observer === f.user) {
    await expect(editor(page).locator(`[data-home-pin="${id}"]`)).toHaveCount(0);
    expect((await page.request.get(`/api/v1/playlists/${id}`)).status()).toBe(404);
  } else {
    await expect(editor(page).locator(`[data-home-pin="${id}"]`)).not.toBeChecked();
  }

  await page.getByRole('button', { name: 'Log out', exact: true }).click();
  await page.waitForURL(url => url.pathname === '/login.html');
  await keyboardLogin(page, actor);
  await home(page);
  await customize(page);
  expect((await names(page))[0]).toBe('Recently played');
  await expect(editor(page).locator(`[data-home-pin="${id}"]`)).toBeChecked();
  expect((await page.request.delete(`/api/v1/playlists/${id}`)).ok()).toBe(true);
  await page.reload();
  await home(page);
  await expect(panel(page).getByRole('button', { name: 'Remove 1 unavailable pins' })).toBeVisible();
  await panel(page).getByRole('button', { name: 'Remove 1 unavailable pins' }).click();
  await customize(page);
  const sectionKeys = await editor(page).locator('[data-home-show]').evaluateAll(inputs =>
    inputs.map(input => (input as HTMLElement).dataset.homeShow!));
  for (const key of sectionKeys) await editor(page).locator(`[data-home-show="${key}"]`).uncheck();
  await expect(panel(page)).toContainText('All home collections are hidden.');
  await editor(page).getByRole('button', { name: 'Restore defaults' }).click();
  expect((await names(page))[0]).toBe('Recently added');
  expect(await names(page)).toContain('Random albums');
  await expect(panel(page).getByRole('button', { name, exact: true })).toHaveCount(0);
  e.record({ feature: 'M39', scenario: 'reorder/hide/pin, personal sections and real audio, reload, playlist failure retry, account isolation and defaults', result: 'pass', data: { id, event, scope, original, updated: await names(page) }, artifacts: ['home-narrow-dark.png', 'home-narrow-light.png', 'pinned-playback.wav'] });
});
