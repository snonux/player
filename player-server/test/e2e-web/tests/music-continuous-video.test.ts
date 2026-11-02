import { test, expect } from '@playwright/test';
import { loadFixture } from './helpers/music-fixture';
import { keyboardLogin, openFolder } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires disposable fixture with Videos set');

test('video completes into the next track, skips artwork, wraps and supports shuffle', async ({ page }) => {
  const fixture = loadFixture();
  await keyboardLogin(page, fixture.admin);
  let videos: any;
  await expect.poll(async () => {
    const sets = await (await page.request.get('/api/sets')).json();
    videos = sets.find((set: any) => set.name === 'Videos');
    return videos?.id;
  }, {timeout: 20_000, message: 'Prepare continuous_video_fixture.py before this suite'}).toBeTruthy();
  await openFolder(page, videos.id, '');
  const card = page.locator('.media-card').filter({ has: page.getByRole('button', { name: '01.mp4', exact: true }) });
  await card.hover();
  await card.getByRole('button', {name: 'Play', exact: true}).click();
  const video = page.locator('#media-video');
  const file = () => page.evaluate(async () => (await import('/js/playback.js' as string)).currentMediaInfo()?.file_name);
  await expect.poll(file).toBe('01.mp4');
  await expect.poll(() => video.evaluate((el: HTMLVideoElement) => el.currentTime)).toBeGreaterThan(0.5);
  for (const title of ['02.mp4', '03.mp4', '01.mp4']) {
    await expect.poll(file, {timeout: 20_000}).toBe(title);
    await expect(video).toBeVisible();
    await expect.poll(() => video.evaluate((el: HTMLVideoElement) => !el.paused && el.currentTime > 0)).toBe(true);
  }
  await page.locator('#header-menu-toggle').click();
  await page.locator('#shuffle-toggle').click();
  await expect(page.locator('#shuffle-toggle')).toHaveClass(/active/);
  const seen = new Set<string>([await file()]);
  for (let i = 0; i < 2; i++) {
    const previous = await file();
    await expect.poll(file, {timeout: 20_000}).not.toBe(previous);
    seen.add(await file());
  }
  expect([...seen].sort()).toEqual(['01.mp4', '02.mp4', '03.mp4']);
  await page.locator('#header-menu-toggle').click();
  await page.locator('#shuffle-toggle').click();
  await expect(page.locator('#shuffle-toggle')).not.toHaveClass(/active/);
});
