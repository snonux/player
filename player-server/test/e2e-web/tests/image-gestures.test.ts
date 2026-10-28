import { test, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
const { genuineTouchPaths } = require('./helpers/genuine-touch.cjs');

test('trusted image swipes navigate and pinch/pan retain the current image', async ({ page }) => {
  const directory = process.env.PLAYER_DISCOVERY_FIXTURE!;
  const { admin } = JSON.parse(readFileSync(join(directory, 'auth.json'), 'utf8'));
  const response = await page.request.post('/api/login', { data: admin });
  expect(response.ok()).toBeTruthy();
  const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="800"><rect width="1200" height="800" fill="red"/></svg>';
  await page.route(/\/api\/media\/9[12](?:\/stream)?$/, route => route.request().url().endsWith('/stream')
    ? route.fulfill({ contentType: 'image/svg+xml', body: svg })
    : route.fulfill({ json: { media: { id: Number(new URL(route.request().url()).pathname.split('/').pop()), type: 'image', file_name: 'gesture.svg' } } }));
  await page.goto('/');
  await page.evaluate(async svg => {
    const { state } = await import('/js/state.js');
    const { loadMediaDirect } = await import('/js/playback.js');
    state.media = [{ id: 91, type: 'image', file_name: 'first.svg' }, { id: 93, type: 'audio', file_name: 'skip.mp3' }, { id: 92, type: 'image', file_name: 'second.svg' }];
    loadMediaDirect(state.media[0], 'data:image/svg+xml,' + encodeURIComponent(svg), '');
    (window as any).trustedTouches = [];
    document.querySelector('#media-image')!.addEventListener('touchstart', event => (window as any).trustedTouches.push(event.isTrusted));
  }, svg);
  const image = page.locator('#media-image');
  await expect(image).toBeVisible();
  const box = (await image.boundingBox())!;
  const x = box.x + box.width / 2, y = box.y + box.height / 2;
  const path = (start: number, end: number, dy = 0) => Array.from({ length: 6 }, (_, i) => ({ x: x + start + (end - start) * i / 5, y: y + dy * i / 5 }));
  await genuineTouchPaths(page, [path(75, -75)]);
  await expect(image).toHaveAttribute('src', /\/api\/media\/92\/stream$/);
  await genuineTouchPaths(page, [path(-75, 75)]);
  await expect(image).toHaveAttribute('src', /\/api\/media\/91\/stream$/);
  await genuineTouchPaths(page, [path(0, 15)]);
  await genuineTouchPaths(page, [path(0, 0, 100)]);
  await expect(image).toHaveAttribute('src', /\/api\/media\/91\/stream$/);
  await genuineTouchPaths(page, [path(-25, -75), path(25, 75)]);
  await expect.poll(() => image.evaluate((img: any) => Number(img.style.transform.match(/scale\(([^)]+)/)?.[1]))).toBeGreaterThan(1.5);
  await genuineTouchPaths(page, [path(75, -75)]);
  await expect(image).toHaveAttribute('src', /\/api\/media\/91\/stream$/);
  await expect.poll(() => image.evaluate((img: any) => img.style.transform)).not.toContain('translate(0px, 0px)');
  await page.keyboard.press('0');
  await expect(image).toHaveCSS('transform', 'matrix(1, 0, 0, 1, 0, 0)');
  await genuineTouchPaths(page, [path(75, -75)]);
  await expect(image).toHaveAttribute('src', /\/api\/media\/92\/stream$/);
  // Automatic advances keep the five-second cadence; only manual interaction
  // pauses the slideshow. Exercise the real playback callback with a fake clock.
  await page.clock.install();
  await page.locator('#btn-slideshow').click();
  await page.clock.fastForward(5_100);
  await expect(image).toHaveAttribute('src', /\/api\/media\/91\/stream$/);
  await page.clock.fastForward(5_100);
  await expect(image).toHaveAttribute('src', /\/api\/media\/92\/stream$/);
  await page.locator('#btn-slideshow').click();
  const trusted = await page.evaluate(() => (window as any).trustedTouches);
  expect(trusted.length).toBeGreaterThanOrEqual(8);
  expect(trusted.every(Boolean)).toBeTruthy();
});
