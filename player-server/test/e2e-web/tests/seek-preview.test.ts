import { test, expect } from '@playwright/test';
import { bootstrap, triggerRescan, waitForScanIdle, waitForServer } from './helpers/server';

test.use({ serviceWorkers: 'block' });
const baseURL = process.env.PLAYER_URL || 'http://localhost:8080';
let adminCookie: string;

test.beforeAll(async () => {
  test.setTimeout(100_000);
  await waitForServer();
  adminCookie = await bootstrap();
  await triggerRescan(adminCookie);
  await waitForScanIdle(adminCookie);
});

test.beforeEach(async ({ page }) => {
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: new URL(baseURL).hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.goto('/');
});

test('video hover previews future and past without seeking playback, preserving click and arrow seeks', async ({ page }) => {
  const response = await page.request.get('/api/v1/media?limit=500');
  expect(response.ok()).toBeTruthy();
  const item = (await response.json()).find((m: { type: string; file_name: string }) => m.type === 'video' && m.file_name.endsWith('.mp4'));
  expect(item, 'fixture library must contain an MP4').toBeTruthy();
  await page.evaluate(async (media) => {
    // @ts-expect-error Browser-native module URL is intentionally runtime-only.
    const playback = await import('/js/playback.js');
    playback.selectAndPlay(media, -1);
  }, item);
  const video = page.locator('#media-video');
  await expect.poll(() => video.evaluate((v: HTMLVideoElement) => Number.isFinite(v.duration) && v.duration > 0 && v.readyState >= 2)).toBe(true);
  const duration = await video.evaluate((v: HTMLVideoElement) => {
    v.pause();
    v.currentTime = v.duration * 0.4;
    return v.duration;
  });
  await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.seeking)).toBe(false);
  const position = await video.evaluate((v: HTMLVideoElement) => v.currentTime);
  const track = page.locator('#progress-track');
  const preview = page.locator('.seek-preview');
  const canvas = preview.locator('canvas');
  const rect = (await track.boundingBox())!;
  const snapshots: string[] = [];
  const fmt = (seconds: number) => {
    const h = Math.floor(seconds / 3600);
    const mm = String(Math.floor(seconds % 3600 / 60)).padStart(2, '0');
    const ss = String(Math.floor(seconds % 60)).padStart(2, '0');
    return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`;
  };
  for (const fraction of [0.75, 0.1]) {
    await page.mouse.move(rect.x + rect.width * fraction, rect.y + rect.height / 2);
    await expect(preview).toBeVisible();
    await expect(canvas).toBeVisible({ timeout: 10_000 });
    await expect(preview.locator('.seek-preview-status')).toHaveText('');
    const painted = await canvas.evaluate((c: HTMLCanvasElement) => [...c.getContext('2d')!.getImageData(0, 0, c.width, c.height).data].some((value, index) => index % 4 === 3 && value > 0));
    expect(painted, 'preview must contain a decoded frame').toBe(true);
    snapshots.push(await canvas.evaluate((c: HTMLCanvasElement) => c.toDataURL()));
    await expect(preview.locator('.seek-preview-time')).toHaveText(fmt(duration * fraction));
    expect(await video.evaluate((v: HTMLVideoElement) => v.currentTime)).toBeCloseTo(position, 2);
    expect(await video.evaluate((v: HTMLVideoElement) => v.paused)).toBe(true);
    const bounds = (await preview.boundingBox())!;
    const player = (await page.locator('#player').boundingBox())!;
    expect(bounds.x).toBeGreaterThanOrEqual(player.x);
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(player.x + player.width + 1);
  }
  expect(snapshots[0], 'past and future must decode different fixture frames').not.toEqual(snapshots[1]);
  await track.click({ position: { x: rect.width / 2, y: rect.height / 2 } });
  await expect.poll(() => video.evaluate((v: HTMLVideoElement) => Math.abs(v.currentTime - v.duration / 2))).toBeLessThan(0.1);
  await track.focus();
  await page.keyboard.press('ArrowLeft');
  await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime)).toBeCloseTo(Math.max(0, duration / 2 - 5), 1);
  await page.mouse.move(rect.x, rect.y - 20);
  await expect(preview).toBeHidden();

  await video.evaluate((v: HTMLVideoElement) => { v.currentTime = v.duration * 0.2; });
  await page.locator('#btn-play').click();
  const playingFrom = await video.evaluate((v: HTMLVideoElement) => v.currentTime);
  await page.mouse.move(rect.x + rect.width * 0.8, rect.y + rect.height / 2);
  await expect(canvas).toBeVisible();
  await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime)).toBeGreaterThan(playingFrom);
  expect(await video.evaluate((v: HTMLVideoElement) => v.paused)).toBe(false);
  expect(await video.evaluate((v: HTMLVideoElement) => v.currentTime)).toBeLessThan(playingFrom + 4);

  await page.locator('#btn-fullscreen').click();
  await expect(page.locator('#player')).toHaveClass(/is-fullscreen/);
  const fullTrack = (await track.boundingBox())!;
  await page.mouse.move(fullTrack.x + fullTrack.width * 0.95, fullTrack.y + fullTrack.height / 2);
  await expect(canvas).toBeVisible();
  const unclipped = await canvas.evaluate((c: HTMLCanvasElement) => {
    const rect = c.getBoundingClientRect();
    // Hit testing otherwise skips the intentional pointer-events:none preview.
    c.style.pointerEvents = 'auto';
    const visible = document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2) === c;
    c.style.removeProperty('pointer-events');
    return visible;
  });
  expect(unclipped, 'fullscreen preview frame must remain visible above the seek bar').toBe(true);
  await page.keyboard.press('Escape');
});

test('audio hover does not create a video preview', async ({ page }) => {
  const response = await page.request.get('/api/v1/media?limit=500');
  const item = (await response.json()).find((m: { type: string }) => m.type === 'audio');
  expect(item, 'fixture library must contain audio').toBeTruthy();
  await page.evaluate(async (media) => {
    // @ts-expect-error Browser-native module URL is intentionally runtime-only.
    const playback = await import('/js/playback.js');
    playback.selectAndPlay(media, -1);
  }, item);
  const track = page.locator('#progress-track');
  await expect(track).toBeVisible();
  await track.hover();
  await expect(page.locator('.seek-preview')).toHaveCount(0);
});

test('a pending or failed preview keeps a useful time without interrupting playback', async ({ page }) => {
  const response = await page.request.get('/api/v1/media?limit=500');
  const item = (await response.json()).find((m: { type: string; file_name: string }) => m.type === 'video' && m.file_name.endsWith('.mp4'));
  expect(item).toBeTruthy();
  await page.evaluate(async (media) => {
    // @ts-expect-error Browser-native module URL is intentionally runtime-only.
    const playback = await import('/js/playback.js');
    playback.selectAndPlay(media, -1);
  }, item);
  const video = page.locator('#media-video');
  await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.readyState >= 2)).toBe(true);
  const position = await video.evaluate((v: HTMLVideoElement) => { v.pause(); return v.currentTime; });
  let pending: import('@playwright/test').Route | undefined;
  await page.route(`**/api/media/${item.id}/stream`, route => { pending = route; });
  await page.locator('#progress-track').hover();
  await expect.poll(() => !!pending).toBe(true);
  const preview = page.locator('.seek-preview');
  await expect(preview.locator('.seek-preview-status')).toHaveText('Loading preview…');
  await expect(preview.locator('.seek-preview-time')).toHaveText(/\d+:\d+/);
  await expect(preview.locator('canvas')).toBeHidden();
  await pending!.fulfill({ status: 410, body: 'gone' });
  await expect(preview.locator('.seek-preview-status')).toHaveText('Preview unavailable');
  expect(await video.evaluate((v: HTMLVideoElement) => v.currentTime)).toBeCloseTo(position, 2);
  expect(await video.evaluate((v: HTMLVideoElement) => v.error)).toBeNull();
  await page.mouse.move(0, 0);
  await expect(preview).toBeHidden();
});
