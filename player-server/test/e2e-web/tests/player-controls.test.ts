import { test, expect } from '@playwright/test';
import { bootstrap, triggerRescan, waitForServer } from './helpers/server';

test.use({ serviceWorkers: 'block' });

const baseURL = process.env.PLAYER_URL || 'http://localhost:8080';
let adminCookie: string;
let audio: { id: number; file_name: string; duration: number };

test.beforeAll(async () => {
  test.setTimeout(100_000);
  await waitForServer();
  adminCookie = await bootstrap();
  await triggerRescan(adminCookie);
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    const response = await fetch(`${baseURL}/api/media?type=audio`, { headers: { Cookie: adminCookie } });
    const media = await response.json() as Array<{ id: number; file_name: string; duration: number }>;
    if (media?.length && media[0].duration > 0) {
      audio = media[0];
      return;
    }
    await new Promise(resolve => setTimeout(resolve, 500));
  }
  throw new Error('Need a scanned audio file with duration for seek test');
});

test('floating, mobile, and fullscreen controls leave seek and volume usable', async ({ page }) => {
  const origin = new URL(baseURL);
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.goto('/');
  await page.evaluate(media => {
    const modulePath = '/js/playback.js';
    return import(modulePath).then(({ loadMediaDirect }) =>
      loadMediaDirect({ ...media, type: 'audio' }, `/api/media/${media.id}/stream`, ''));
  }, audio);

  const player = page.locator('#player');
  const track = page.locator('#progress-track');
  const volume = page.locator('#volume-slider');
  const mute = page.locator('#btn-mute');
  const fullscreen = page.locator('#btn-fullscreen');
  await expect(player).toHaveClass(/open/);
  await expect.poll(() => page.locator('#media-audio').evaluate((m: any) => m.duration)).toBeGreaterThan(0);
  await page.locator('#media-audio').evaluate((m: any) => m.pause());

  async function assertControls(minTrackWidth: number) {
    const playerBox = await player.boundingBox();
    expect(playerBox).not.toBeNull();
    const trackBox = await track.boundingBox();
    expect(trackBox).not.toBeNull();
    expect(trackBox!.width).toBeGreaterThan(minTrackWidth);
    expect(trackBox!.height).toBeGreaterThanOrEqual(24);
    for (const control of [volume, mute, fullscreen]) {
      await expect(control).toBeVisible();
      const box = await control.boundingBox();
      expect(box).not.toBeNull();
      expect(box!.x).toBeGreaterThanOrEqual(playerBox!.x);
      expect(box!.x + box!.width).toBeLessThanOrEqual(playerBox!.x + playerBox!.width);
      expect(box!.y + box!.height).toBeLessThanOrEqual(playerBox!.y + playerBox!.height);
    }
  }

  async function seekTo(fraction: number, nearTop = false) {
    const box = await track.boundingBox();
    expect(box).not.toBeNull();
    await track.click({ position: { x: box!.width * fraction, y: nearTop ? 2 : box!.height / 2 } });
    await expect.poll(async () => {
      const actual = await page.locator('#media-audio').evaluate((m: any) => m.currentTime / m.duration);
      return Math.abs(actual - fraction);
    }).toBeLessThan(0.15);
  }

  await assertControls(80);
  await seekTo(0.7);
  const volumeBox = await volume.boundingBox();
  expect(volumeBox).not.toBeNull();
  await volume.click({ position: { x: volumeBox!.width * 0.3, y: volumeBox!.height / 2 } });
  await expect.poll(() => page.locator('#media-audio').evaluate((m: any) => m.volume)).toBeLessThan(0.5);
  await expect.poll(() => page.locator('#media-audio').evaluate((m: any) => m.volume)).toBeGreaterThan(0);

  await fullscreen.click();
  await expect(player).toHaveClass(/is-fullscreen/);
  await expect.poll(() => page.evaluate(() => (globalThis as any).document.fullscreenElement?.id)).toBe('player');
  await assertControls(120);
  await seekTo(0.25);
  await fullscreen.click();
  await expect(player).not.toHaveClass(/is-fullscreen/);

  await page.setViewportSize({ width: 320, height: 640 });
  await assertControls(80);
  await seekTo(0.6, true);
  await mute.click();
  await expect.poll(() => page.locator('#media-audio').evaluate((m: any) => m.muted)).toBe(true);
  await mute.click();
  await expect.poll(() => page.locator('#media-audio').evaluate((m: any) => m.muted)).toBe(false);
  await player.evaluate((element: any) => element.style.setProperty('--floating-w', '240px'));
  await expect.poll(async () => (await player.boundingBox())?.width).toBeLessThan(243);
  await assertControls(80);
  await seekTo(0.4);
});
