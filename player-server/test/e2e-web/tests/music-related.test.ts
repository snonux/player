import { expect, request, test, type Page } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { audioState, captureOutput, assertValidCapture, keyboardLogin, mainSegment, openFolder, openSidebarMusic, resetProgress, selectByKeys } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires an isolated local music fixture');
let fixture: MusicFixture;
let evidence: Evidence;

test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  evidence = new Evidence(fixture.evidenceDir, info.project.name, {
    browser_version: browser.version(), server_version: fixture.serverVersion,
    server_build: fixture.buildRevision, checkout: fixture.checkout,
  });
});
test.beforeEach(async ({ page }) => {
  await page.addInitScript(installAudioTap);
  await keyboardLogin(page, fixture.user);
});

const related = (page: Page) => page.locator('.music-related');
async function openAlbum(page: Page, name: string, artist: string) {
  await openSidebarMusic(page, 'Albums');
  // Startup can finish its initial Library load after the first sidebar click.
  // Wait for the catalog and repeat that visible action once if it won the race.
  if (!await page.locator('.album-card').first().waitFor({ state: 'visible', timeout: 4_000 }).then(() => true, () => false)) {
    await openSidebarMusic(page, 'Albums');
  }
  await expect(page.locator('.album-card').first()).toBeVisible();
  await page.getByRole('button', { name: `Open album ${name} by ${artist}`, exact: true }).click();
  await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
  await expect(related(page).getByRole('heading', { name: 'More like this' })).toBeVisible();
}
async function openArtist(page: Page, name: string) {
  await openSidebarMusic(page, 'Artists');
  if (!await page.locator('[data-artist-card]').first().waitFor({ state: 'visible', timeout: 4_000 }).then(() => true, () => false)) {
    await openSidebarMusic(page, 'Artists');
  }
  await expect(page.locator('[data-artist-card]').first()).toBeVisible();
  await page.getByRole('button', { name: `Open artist ${name}`, exact: true }).click();
  await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
  await expect(related(page).getByRole('heading', { name: 'More like this' })).toBeVisible();
}
async function checkOutput(page: Page, id: number, hz: number, name: string, artifacts: string[]) {
  await expect.poll(() => page.evaluate(async expected => {
    const { currentMediaId } = await import('/js/playback.js');
    const audio = document.getElementById('media-audio') as HTMLAudioElement;
    return currentMediaId() === expected && !audio.paused && audio.currentTime > .5;
  }, id), { timeout: 20_000 }).toBe(true);
  const path = evidence.path(`${name}.wav`);
  const output = await captureOutput(page, 1500, path);
  assertValidCapture(output);
  const segment = mainSegment(output.analysis);
  expect(segment?.duration_s).toBeGreaterThan(.7);
  expect(Math.abs(segment!.tone_hz! - hz)).toBeLessThan(5);
  artifacts.push(path);
}
async function minimize(page: Page) {
  const classes = await page.locator('#player').getAttribute('class') || '';
  if (classes.includes('open') && !classes.includes('minimized')) {
    await page.getByRole('button', { name: 'Minimize player (Shift+C)', exact: true }).click();
  }
}

test('M41 album, artist and song suggestions open and play actual local sources', async ({ page }) => {
  const artifacts: string[] = [];
  try {
    const tokyo = track(fixture, 'Music/Ünïcødé Artïst - 東京 Nights/01 - Café Ñoño.mp3');
    const afterglow = track(fixture, 'Music/Various Artists - Synth Compilation/Disc 2/01 - Afterglow.ogg');
    await openAlbum(page, 'Night Transit', 'Aurora Lane');
    const album = related(page);
    await expect(album).toContainText('From your library · shared music tags');
    await expect(album.getByRole('button', { name: 'Open album Night Transit' })).toHaveCount(0);
    await expect(album.getByRole('button', { name: 'Open album 東京 Nights (夜)' })).toBeVisible();
    await expect(album).toContainText('Genre: Ambient');
    expect(await audioState(page)).toMatchObject({ paused: true });
    for (const [theme, width] of [['dark', 1366], ['light', 390]] as const) {
      await page.setViewportSize({ width, height: 844 });
      if (await page.locator('html').getAttribute('data-theme') !== theme)
        await page.getByRole('button', { name: 'Toggle theme', exact: true }).click();
      const screenshot = evidence.path(`m41-album-${theme}-${width}.png`);
      await page.screenshot({ path: screenshot, fullPage: true, animations: 'disabled' });
      artifacts.push(screenshot);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    }
    const openTokyo = album.getByRole('button', { name: 'Open album 東京 Nights (夜)' });
    await openTokyo.focus();
    await page.keyboard.press('Enter');
    await expect(page.getByRole('heading', { name: '東京 Nights (夜)', exact: true })).toBeVisible();
    await page.keyboard.press('Backspace');
    await expect(page.getByRole('heading', { name: 'Night Transit', exact: true })).toBeVisible();
    await resetProgress(page, tokyo.id);
    await related(page).getByRole('button', { name: 'Play 東京 Nights (夜)' }).click();
    await checkOutput(page, tokyo.id, tokyo.tone_hz[0], 'm41-album-play', artifacts);
    await minimize(page);

    await openArtist(page, 'Aurora Lane');
    const artist = related(page);
    await expect(artist.getByRole('button', { name: 'Open artist Aurora Lane', exact: true })).toHaveCount(0);
    await expect(artist.getByRole('button', { name: 'Open artist Dusk Choir' })).toBeVisible();
    await artist.getByRole('button', { name: 'Open artist Dusk Choir' }).click();
    await expect(page.getByRole('heading', { name: 'Dusk Choir', exact: true })).toBeVisible();
    await page.keyboard.press('Backspace');
    await expect(page.getByRole('heading', { name: 'Aurora Lane', exact: true })).toBeVisible();
    await resetProgress(page, afterglow.id);
    await related(page).getByRole('button', { name: 'Play Dusk Choir' }).click();
    await checkOutput(page, afterglow.id, afterglow.tone_hz[0], 'm41-artist-play', artifacts);
    await minimize(page);

    const first = track(fixture, 'Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3');
    await openFolder(page, fixture.setIds.Music, 'Aurora Lane - Night Transit (2021)');
    await selectByKeys(page, first.title);
    await page.keyboard.press('i');
    await expect(page.locator('#media-info-modal')).toHaveClass(/open/);
    const song = related(page);
    await expect(song.getByRole('button', { name: 'Open song First Light' })).toHaveCount(0);
    await expect(song.getByRole('button', { name: 'Open song Long Road Home' })).toBeVisible();
    await song.getByRole('button', { name: 'Open song Long Road Home' }).click();
    await expect(page.locator('#media-info-body')).toContainText('Long Road Home');
    await resetProgress(page, first.id);
    await related(page).getByRole('button', { name: 'Play First Light' }).click();
    await checkOutput(page, first.id, first.tone_hz[0], 'm41-song-play', artifacts);
    await page.keyboard.press('Escape');
    await expect(page.locator('#media-info-modal')).not.toHaveClass(/open/);
    await expect.poll(() => page.evaluate(() => {
      const selected = document.querySelector('#media-grid .selected');
      return !!selected && selected.contains(document.activeElement);
    })).toBe(true);
    await page.keyboard.press('/');
    await expect(page.getByRole('searchbox', { name: 'Search media' })).toBeFocused();
    await page.keyboard.type('First Light');
    await expect(page.getByRole('searchbox', { name: 'Search media' })).toHaveValue('First Light');
    evidence.record({ feature: 'M41', scenario: 'album/artist/song result navigation, source exclusion, keyboard/focus, themes/narrow, actual output', result: 'pass', artifacts });
  } catch (error) {
    evidence.record({ feature: 'M41', scenario: 'related happy paths', result: 'fail', detail: String(error), artifacts });
    throw error;
  }
});

test('M41 empty, actual permission denial and offline read failure recover through Retry', async ({ page, context }) => {
  const artifacts: string[] = [];
  const admin = await request.newContext({ baseURL: fixture.origin });
  let permission: { set_id: number; user_id: number } | undefined;
  try {
    await openAlbum(page, 'Partial Metadata', 'Unknown artist');
    await expect(related(page)).toContainText('No related music found. Add genre or artist tags');
    await openFolder(page, fixture.setIds.Music, 'Untagged');
    const untagged = track(fixture, 'Music/Untagged/track_0042.mp3');
    await selectByKeys(page, untagged.title);
    await page.keyboard.press('i');
    await expect(related(page)).toContainText('No related music found. Add genre or artist tags');
    await page.keyboard.press('Escape');

    // Hold only the real related read until the server ACL changes. The source
    // detail has already loaded, so its error and Retry remain visible.
    let release!: () => void;
    let intercepted!: () => void;
    const gate = new Promise<void>(resolve => { release = resolve; });
    const seen = new Promise<void>(resolve => { intercepted = resolve; });
    await page.route('**/api/music/related/album/**', async route => {
      intercepted();
      await gate;
      await route.continue();
    });
    const opening = openAlbum(page, 'Night Transit', 'Aurora Lane');
    await seen;
    expect((await admin.post('/api/v1/auth/login', { data: fixture.admin })).ok()).toBe(true);
    const users = await (await admin.get('/api/v1/admin/users')).json();
    permission = { set_id: fixture.setIds.Music, user_id: users.find((u: any) => u.username === fixture.user.username).id };
    expect((await admin.delete('/api/v1/admin/permissions', { data: permission })).ok()).toBe(true);
    release();
    await opening;
    await expect(related(page).getByRole('alert')).toContainText('Could not load related music');
    const source = track(fixture, 'Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3');
    expect((await page.request.get(`/api/v1/music/related/song/${source.id}`)).status()).toBe(404);
    expect((await page.request.get(`/api/v1/music/related/song/${fixture.mediaIds['Restricted/Admin Only - Hidden Album/01 - Hidden Track.mp3']}`)).status()).toBe(404);
    const denied = evidence.path('m41-denied.png');
    await page.screenshot({ path: denied, fullPage: true }); artifacts.push(denied);
    expect((await admin.post('/api/v1/admin/permissions', { data: { ...permission, role: 'viewer' } })).ok()).toBe(true);
    permission = undefined;
    await page.unrouteAll({ behavior: 'wait' });
    await related(page).getByRole('button', { name: 'Retry' }).click();
    await expect(related(page).getByRole('button', { name: 'Open album Synth Compilation' })).toBeVisible();

    await page.getByRole('button', { name: '← Back to albums', exact: true }).click();
    let releaseOffline!: () => void;
    let seenOffline!: () => void;
    const offlineGate = new Promise<void>(resolve => { releaseOffline = resolve; });
    const offlineSeen = new Promise<void>(resolve => { seenOffline = resolve; });
    await page.route('**/api/music/related/album/**', async route => {
      seenOffline();
      await offlineGate;
      await route.continue();
    });
    const reopening = openAlbum(page, 'Night Transit', 'Aurora Lane');
    await offlineSeen;
    await context.setOffline(true);
    try {
      releaseOffline();
      await reopening;
      await expect(related(page).getByRole('alert')).toContainText('Could not load related music');
      const offline = evidence.path('m41-offline.png');
      await page.screenshot({ path: offline, fullPage: true }); artifacts.push(offline);
    } finally { await context.setOffline(false); await page.unrouteAll({ behavior: 'wait' }); }
    await related(page).getByRole('button', { name: 'Retry' }).click();
    await expect(related(page).getByRole('button', { name: 'Open album Synth Compilation' })).toBeVisible();
    evidence.record({ feature: 'M41', scenario: 'untagged empty, real ACL 404 and Retry, browser offline read and recovery', result: 'pass', artifacts });
  } catch (error) {
    evidence.record({ feature: 'M41', scenario: 'empty and read boundaries', result: 'fail', detail: String(error), artifacts });
    throw error;
  } finally {
    if (permission) await admin.post('/api/v1/admin/permissions', { data: { ...permission, role: 'viewer' } });
    await admin.dispose();
  }
});
