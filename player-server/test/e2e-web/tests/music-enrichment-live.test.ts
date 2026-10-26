import { test, expect, request, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { Evidence, loadFixture, SUPPORT_DIR, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, openSidebarMusic, audioState, captureOutput, assertValidCapture, mainSegment } from './helpers/music-ui';

const modeFile = process.env.PLAYER_M10_MODE_FILE || '/tmp/player-m10-mode';
let fixture: MusicFixture, evidence: Evidence;
test.skip(!process.env.PLAYER_MUSIC_FIXTURE || !process.env.PLAYER_M10_ISOLATED,
  'Requires disposable music fixture with MUSIC_ENRICHMENT_URL and local provider');
test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  evidence = new Evidence(fixture.evidenceDir, info.project.name, {
    browser_version: browser.version(), server_version: fixture.serverVersion,
    server_build: fixture.buildRevision, checkout: fixture.checkout,
  });
});
test.beforeEach(async ({ page }) => {
  await page.addInitScript(installAudioTap);
  await keyboardLogin(page, fixture.admin);
});

function about(page: Page) { return page.locator('section.music-enrichment'); }
async function openAbout(page: Page, keyboard = true) {
  const section = about(page);
  await expect(section).toBeVisible();
  const toggle = section.getByRole('button', { name: 'About', exact: true });
  if (keyboard) { await toggle.focus(); await page.keyboard.press('Enter'); }
  else await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  return section;
}
async function refresh(page: Page, section: ReturnType<typeof about>) {
  const button = section.getByRole('button', { name: 'Refresh metadata', exact: true });
  await button.click();
  await expect(button).toBeFocused();
  return section.locator('[role="status"]');
}

test('M10 artist/album enrichment refresh, no match, failure, ACL and playback', async ({ page, context }) => {
  const artifacts: string[] = [];
  const admin = await request.newContext({ baseURL: fixture.origin });
  let revoked = false, userId = 0;
  try {
    writeFileSync(modeFile, 'ready');
    expect((await admin.post('/api/v1/auth/login', { data: fixture.admin })).ok()).toBe(true);
    const artists = await (await page.request.get('/api/v1/music/artists')).json();
    const person = artists.find((artist: any) => artist.name === 'Aurora Lane');
    expect(person).toBeTruthy();
    await openSidebarMusic(page, 'Artists');
    const artistCard = page.getByRole('button', { name: 'Open artist Aurora Lane', exact: true });
    await artistCard.focus(); await page.keyboard.press('Enter');
    await expect(page.getByRole('heading', { name: 'Aurora Lane', exact: true })).toBeVisible();
    const artist = await openAbout(page);
    const artistStatus = await refresh(page, artist);
    await expect(artistStatus).toContainText('Fixture artist description for Aurora Lane.');
    await expect(artistStatus).toContainText('Source: Local deterministic fixture');
    await expect(artistStatus.getByRole('link', { name: /MusicBrainz/ })).toHaveAttribute('href', 'https://musicbrainz.org/');
    await expect.poll(() => artistStatus.locator('img').evaluate((image: HTMLImageElement) => image.naturalWidth)).toBeGreaterThan(0);
    for (const [theme, width] of [['dark', 1366], ['light', 390]] as const) {
      await page.setViewportSize({ width, height: 844 });
      if (await page.locator('html').getAttribute('data-theme') !== theme)
        await page.getByRole('button', { name: 'Toggle theme', exact: true }).click();
      await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      const shot = evidence.path(`m10-artist-${theme}-${width}.png`);
      await page.screenshot({ path: shot, fullPage: true, animations: 'disabled' }); artifacts.push(shot);
    }
    await page.getByRole('button', { name: '▶ Play artist', exact: true }).click();
    await expect.poll(async () => (await audioState(page)).paused).toBe(false);
    const before = await audioState(page);
    const capture = await captureOutput(page, 1600, evidence.path('m10-about-source.wav'));
    assertValidCapture(capture);
    expect(mainSegment(capture.analysis)?.duration_s).toBeGreaterThan(1);
    artifacts.push(capture.wav);
    await expect.poll(async () => (await audioState(page)).time).toBeGreaterThan(before.time + 0.5);
    await page.keyboard.press('Backspace');
    await openSidebarMusic(page, 'Albums');
    const albumCard = page.getByRole('button', { name: 'Open album Night Transit by Aurora Lane', exact: true });
    await albumCard.click();
    await expect(page.getByRole('heading', { name: 'Night Transit', exact: true })).toBeVisible();
    const localCover = page.locator('.album-header > img.album-cover');
    await expect(localCover).toBeVisible();
    const localSource = await localCover.getAttribute('src');
    const album = await openAbout(page, false);
    const albumStatus = await refresh(page, album);
    await expect(albumStatus).toContainText('Fixture album description for Night Transit.');
    expect(await localCover.getAttribute('src')).toBe(localSource);

    writeFileSync(modeFile, 'failure');
    await refresh(page, album);
    await expect(albumStatus).toContainText('Fixture album description for Night Transit.');
    await expect(albumStatus).toContainText('Metadata refresh failed.');
    expect(await localCover.getAttribute('src')).toBe(localSource);
    writeFileSync(modeFile, 'empty');
    await refresh(page, album);
    await expect(albumStatus).toContainText('No metadata found for this artist or album.');
    await expect(albumStatus).not.toContainText('Fixture album description');
    expect(await localCover.getAttribute('src')).toBe(localSource);
    writeFileSync(modeFile, 'ready');
    await refresh(page, album);
    await expect(albumStatus).toContainText('Fixture album description for Night Transit.');

    await context.setOffline(true);
    try {
      await refresh(page, album);
      await expect(albumStatus).toContainText('Fixture album description for Night Transit.');
      await expect(album.locator('[data-enrichment-error]')).toContainText('Could not load metadata.');
    } finally { await context.setOffline(false); }
    await refresh(page, album);
    await expect(album.locator('[data-enrichment-error]')).toHaveCount(0);
    execFileSync('python3', [join(SUPPORT_DIR, 'music_fixture.py'), 'restart', fixture.dir], {
      env: { ...process.env, MUSIC_ENRICHMENT_URL: 'http://127.0.0.1:18910/metadata' },
    });
    await page.reload();
    const persisted = await openAbout(page);
    await expect(persisted.locator('[role="status"]')).toContainText('Fixture album description for Night Transit.');
    expect(await page.locator('.album-header > img.album-cover').getAttribute('src')).toBe(localSource);
    const shot = evidence.path('m10-album-after-restart.png');
    await page.screenshot({ path: shot, fullPage: true, animations: 'disabled' }); artifacts.push(shot);

    await page.getByRole('button', { name: 'Log out', exact: true }).click();
    await keyboardLogin(page, fixture.user);
    await openSidebarMusic(page, 'Albums');
    await page.getByRole('button', { name: 'Open album Night Transit by Aurora Lane', exact: true }).click();
    const viewer = await openAbout(page);
    await expect(viewer.locator('[role="status"]')).toContainText('Fixture album description for Night Transit.');
    await expect(viewer.getByRole('button', { name: 'Refresh metadata' })).toHaveCount(0);
    const albumKey = (await (await page.request.get('/api/v1/music/albums')).json())
      .find((entry: any) => entry.title === 'Night Transit').album_key;
    expect((await page.request.post(`/api/v1/music/enrichment/album/${albumKey}`)).status()).toBe(403);
    const users = await (await admin.get('/api/v1/admin/users')).json();
    userId = users.find((entry: any) => entry.username === fixture.user.username).id;
    expect((await admin.delete('/api/v1/admin/permissions', {
      data: { set_id: fixture.setIds.Music, user_id: userId },
    })).ok()).toBe(true);
    revoked = true;
    expect((await page.request.get(`/api/v1/music/enrichment/album/${albumKey}`)).status()).toBe(404);
    await page.reload();
    await expect(page.getByText('Fixture album description for Night Transit.')).toHaveCount(0);
    evidence.record({ feature: 'M10', scenario: 'artist and album provider refresh, artwork, playback, failure, no match, offline, restart and ACL', result: 'pass', artifacts });
  } catch (error) {
    evidence.record({ feature: 'M10', scenario: 'artist and album enrichment', result: 'fail', detail: String(error), artifacts });
    throw error;
  } finally {
    if (revoked) expect((await admin.post('/api/v1/admin/permissions', {
      data: { set_id: fixture.setIds.Music, user_id: userId, role: 'viewer' },
    })).ok()).toBe(true);
    await admin.dispose();
    writeFileSync(modeFile, 'ready');
  }
});
