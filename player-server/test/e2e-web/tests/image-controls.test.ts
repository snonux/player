import { test, expect } from '@playwright/test';
import { bootstrap, triggerRescan, waitForServer } from './helpers/server';

test.use({ serviceWorkers: 'block', hasTouch: true, viewport: { width: 320, height: 640 } });

const baseURL = process.env.PLAYER_URL || 'http://localhost:8080';
let adminCookie: string;
let media: Array<{ id: number; type: string; file_name: string }>;

test.beforeAll(async () => {
  test.setTimeout(100_000);
  await waitForServer();
  adminCookie = await bootstrap();
  await triggerRescan(adminCookie);
  const headers = { Cookie: adminCookie };
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    const images = await (await fetch(`${baseURL}/api/media?type=image`, { headers })).json() as Array<{ id: number; type: string; file_name: string }> | null;
    const audio = await (await fetch(`${baseURL}/api/media?type=audio`, { headers })).json() as Array<{ id: number; type: string; file_name: string }> | null;
    if (images && images.length >= 2 && audio?.length) {
      media = [images[0], audio[0], images[1]];
      return;
    }
    await new Promise(resolve => setTimeout(resolve, 500));
  }
  throw new Error('Need two scanned images and one audio item');
});

test('image zoom and slideshow work by touch and keyboard without sharing', async ({ page }) => {
  const origin = new URL(baseURL);
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.clock.install();
  await page.goto('/');
  await page.evaluate(async items => {
    const statePath = '/js/state.js';
    const playbackPath = '/js/playback.js';
    const { state } = await import(statePath);
    const { selectAndPlay } = await import(playbackPath);
    state.media = items;
    selectAndPlay(items[0], 0);
  }, media);

  const player = page.locator('#player');
  const image = page.locator('#media-image');
  const zoomIn = page.locator('#btn-zoom-in');
  const zoomOut = page.locator('#btn-zoom-out');
  const slideshow = page.locator('#btn-slideshow');
  await expect(player).toHaveClass(/open.*has-image/);
  const playerBox = await player.boundingBox();
  expect(playerBox).not.toBeNull();
  expect(playerBox!.x + playerBox!.width).toBeLessThanOrEqual(320);
  for (const control of [zoomIn, zoomOut, slideshow]) {
    await expect(control).toBeVisible();
    const box = await control.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.width).toBeGreaterThanOrEqual(40);
    expect(box!.height).toBeGreaterThanOrEqual(40);
    expect(box!.x + box!.width).toBeLessThanOrEqual(320);
  }

  await zoomIn.tap();
  await expect(image).toHaveCSS('transform', /matrix\(1\.25/);
  await zoomOut.tap();
  await expect(image).toHaveCSS('transform', /matrix\(1, 0, 0, 1/);
  await page.keyboard.press('+');
  await expect(image).toHaveCSS('transform', /matrix\(1\.25/);
  await page.keyboard.press('-');
  await expect(image).toHaveCSS('transform', /matrix\(1, 0, 0, 1/);
  await page.locator('#btn-next').tap();
  await expect(image).toHaveAttribute('src', new RegExp(`/api/media/${media[2].id}/stream$`));
  await page.locator('#btn-prev').tap();
  await expect(image).toHaveAttribute('src', new RegExp(`/api/media/${media[0].id}/stream$`));

  const sharesBefore = await (await page.request.get('/api/shares')).json();
  await page.keyboard.press('Shift+S');
  await expect(slideshow).toHaveAttribute('aria-label', 'Pause slideshow');
  await page.clock.fastForward(5_100);
  await expect(image).toHaveAttribute('src', new RegExp(`/api/media/${media[2].id}/stream$`));
  await page.keyboard.press('Shift+S');
  await expect(slideshow).toHaveAttribute('aria-label', 'Start slideshow');
  await page.clock.fastForward(5_100);
  await expect(image).toHaveAttribute('src', new RegExp(`/api/media/${media[2].id}/stream$`));
  const sharesAfter = await (await page.request.get('/api/shares')).json();
  expect(sharesAfter).toEqual(sharesBefore);

  await slideshow.tap();
  await expect(slideshow).toHaveAttribute('aria-label', 'Pause slideshow');
  await page.locator('#btn-next').tap();
  await expect(image).toHaveAttribute('src', new RegExp(`/api/media/${media[0].id}/stream$`));
  await page.clock.fastForward(5_100);
  await expect(image).toHaveAttribute('src', new RegExp(`/api/media/${media[0].id}/stream$`));
  await page.keyboard.press('ArrowRight');
  await expect(image).toHaveAttribute('src', new RegExp(`/api/media/${media[2].id}/stream$`));
  await page.clock.fastForward(5_100);
  await expect(image).toHaveAttribute('src', new RegExp(`/api/media/${media[2].id}/stream$`));
  await slideshow.tap();
  await expect(slideshow).toHaveAttribute('aria-label', 'Start slideshow');

  await page.evaluate(async () => {
    const modulePath = '/js/views/shares.js';
    const { toggleShares } = await import(modulePath);
    await toggleShares();
  });
  await expect(page.locator('#shares-modal')).toHaveClass(/open/);
  await page.keyboard.press('Escape');
  await expect(page.locator('#shares-modal')).not.toHaveClass(/open/);
  await expect(player).toHaveClass(/open/);

  await slideshow.tap();
  await page.keyboard.press('Escape');
  await expect(player).not.toHaveClass(/open/);
  expect(await page.evaluate(async () => {
    const modulePath = '/js/imageViewer.js';
    const { isSlideshowActive } = await import(modulePath);
    return isSlideshowActive();
  })).toBe(false);
  await page.clock.fastForward(5_100);
  await expect(player).not.toHaveClass(/open/);

  await page.evaluate(id => {
    const document = (globalThis as any).document;
    const card = document.createElement('div');
    card.className = 'media-card selected';
    card.dataset.id = String(id);
    document.body.append(card);
  }, media[0].id);
  const createdShare = page.waitForResponse(response =>
    response.url().includes(`/api/media/${media[0].id}/shares`) && response.request().method() === 'POST');
  await page.keyboard.press('s');
  expect((await createdShare).ok()).toBe(true);
});
