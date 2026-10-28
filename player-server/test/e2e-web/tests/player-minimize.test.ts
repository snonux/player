import { test, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { bootstrap, waitForServer } from './helpers/server';

test.use({ serviceWorkers: 'block' });

let adminCookie: string;

test.beforeAll(async () => {
  await waitForServer();
  adminCookie = await bootstrap();
});

for (const key of ['Escape', 'Backspace']) {
  for (const type of ['audio', 'video', 'image']) {
    test(`${key} unwinds fullscreen and ${type} view back to thumbnails`, async ({ page }) => {
      const origin = new URL(process.env.PLAYER_URL || 'http://localhost:8080');
      await page.context().addCookies([{
        name: 'session', value: adminCookie.slice('session='.length),
        domain: origin.hostname, path: '/', sameSite: 'Strict',
      }]);
      await page.goto('/');
      await expect(page.locator('#header-menu-toggle')).toBeVisible();
      await expect(page.locator('#media-grid')).toBeVisible();
      await page.evaluate(async type => {
        const doc = (globalThis as any).document;
        const grid = doc.getElementById('media-grid');
        grid.classList.remove('hidden');
        grid.innerHTML = '<div class="media-card selected" tabindex="0" data-id="1">Sample</div>';
        const modulePath = '/js/playback.js';
        const { loadMediaDirect } = await import(modulePath);
        loadMediaDirect({ id: 1, type, file_name: `sample.${type}`, duration: 1 },
          type === 'image' ? 'data:image/gif;base64,R0lGODlhAQABAAD/ACwAAAAAAQABAAACADs=' : `data:${type}/mpeg;base64,AAAA`, '');
      }, type);
      const player = page.locator('#player');
      const card = page.locator('.media-card.selected');
      await expect(player).toHaveClass(/open/);
      // Help takes precedence and leaves the player and selection intact.
      await page.evaluate(async () => {
        const helpPath = '/js/views/help.js';
        (await import(helpPath)).toggleHelp();
      });
      await expect(page.locator('#help-modal')).toHaveClass(/open/);
      await page.keyboard.press('Escape');
      await expect(page.locator('#help-modal')).not.toHaveClass(/open/);
      await expect(player).not.toHaveClass(/minimized/);
      await expect(player).toHaveClass(/open/);
      await expect(card).toBeFocused();

      await page.locator('#btn-fullscreen').click();
      await expect.poll(() => page.evaluate(() => Boolean((globalThis as any).document.fullscreenElement))).toBe(true);
      await page.keyboard.press(key);
      await expect.poll(() => page.evaluate(() => Boolean((globalThis as any).document.fullscreenElement))).toBe(false);
      await expect(player).not.toHaveClass(/is-fullscreen|minimized/);
      await expect(player).toHaveClass(/open/);
      // The progress control keeps focus after seeking; Escape must still
      // reach the global handler (image controls use the lightbox handler).
      await page.locator(type === 'image' ? '#btn-zoom-in' : '#progress-track').focus();
      await page.keyboard.press(key);
      if (type === 'image') await expect(player).not.toHaveClass(/open/);
      else await expect(player).toHaveClass(/minimized/);
      await expect(card).toBeFocused();
      await page.keyboard.press('Escape');
      await expect(card).toHaveClass(/selected/);
      if (type !== 'image') await expect(player).toHaveClass(/minimized/);
    });
  }
}

test('Backspace keeps text editing and folder navigation', async ({ page }) => {
  const origin = new URL(process.env.PLAYER_URL || 'http://localhost:8080');
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.goto('/');
  await expect(page.locator('#header-menu-toggle')).toBeVisible();
  await expect(page.locator('#media-grid')).toBeVisible();
  const search = page.locator('#search-input');
  await page.keyboard.press('/');
  await search.fill('abc');
  await search.press('Backspace');
  await expect(search).toHaveValue('ab');
  await search.fill('');
  await page.evaluate(async () => {
    const statePath = '/js/state.js';
    const { state } = await import(statePath);
    state.folderPath = 'one/two';
  });
  await search.press('Escape');
  await page.keyboard.press('Backspace');
  await expect.poll(() => page.evaluate(async () => {
    const statePath = '/js/state.js';
    return (await import(statePath)).state.folderPath;
  })).toBe('one');
});

test('Minimize (in options) and Restore work for audio, video, and image', async ({ page }) => {
  const origin = new URL(process.env.PLAYER_URL || 'http://localhost:8080');
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.goto('/');

  const { minimizePlayer, playerOptionsItem } = await import('./helpers/player-options');
  const player = page.locator('#player');
  const restore = page.locator('#btn-restore-player');
  for (const [type, fileName, url] of [
    ['audio', 'sample.mp3', 'data:audio/mpeg;base64,AAAA'],
    ['video', 'sample.mp4', 'data:video/mp4;base64,AAAA'],
    ['image', 'sample.gif', 'data:image/gif;base64,R0lGODlhAQABAAD/ACwAAAAAAQABAAACADs='],
  ]) {
    await page.evaluate(async args => {
      const modulePath = '/js/playback.js';
      const { loadMediaDirect } = await import(modulePath);
      loadMediaDirect({ id: 1, type: args.type, file_name: args.fileName, duration: 1 }, args.url, '');
    }, { type, fileName, url });
    await expect(player).toHaveClass(/open/);
    await expect(page.locator('#btn-player-options')).toBeVisible();
    await expect(page.locator('#btn-minimize')).toBeHidden();
    await minimizePlayer(page);
    await expect(player).toHaveClass(/minimized/);
    await expect(restore).toBeVisible();
    await expect(restore).toContainText(`Restore: ${fileName}`);
    await restore.click();
    await expect(player).not.toHaveClass(/minimized/);
    await expect(await playerOptionsItem(page, '#btn-minimize')).toBeVisible();
    await page.keyboard.press('Escape');
  }
});

test('Escape minimizes real audio and video without interrupting playback', async ({ page }) => {
  const origin = new URL(process.env.PLAYER_URL || 'http://localhost:8080');
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.goto('/');
  for (const [type, file, contentType] of [
    ['audio', 'audiobooks/aesops-fables/10-chapter.mp3', 'audio/mpeg'],
    ['video', 'videos/nile-red-sea-nasa.mp4', 'video/mp4'],
  ]) {
    const url = `/escape-fixture-${type}`;
    await page.route(url, route => route.fulfill({
      contentType, body: readFileSync(resolve(__dirname, '../../../testdata/media', file)),
    }));
    await page.evaluate(async ({ type, url }) => {
      const modulePath = '/js/playback.js';
      (await import(modulePath)).loadMediaDirect({ id: 1, type, file_name: `sample.${type}` }, url, '');
    }, { type, url });
    const media = page.locator(`#media-${type}`);
    await expect.poll(() => media.evaluate((m: any) => m.readyState)).toBeGreaterThanOrEqual(2);
    await media.evaluate(async (m: any) => { m.muted = true; await m.play(); });
    await expect.poll(() => media.evaluate((m: any) => m.currentTime)).toBeGreaterThan(0);
    await page.locator('#btn-play').focus();
    await page.keyboard.press('Escape');
    await expect(page.locator('#player')).toHaveClass(/minimized/);
    expect(await media.evaluate((m: any) => m.paused)).toBe(false);
    const position = await media.evaluate((m: any) => m.currentTime);
    await expect.poll(() => media.evaluate((m: any) => m.currentTime)).toBeGreaterThan(position + 0.2);
    await media.evaluate((m: any) => m.pause());
  }
});

test('Escape closes an active image slideshow and cancels further advances', async ({ page }) => {
  const origin = new URL(process.env.PLAYER_URL || 'http://localhost:8080');
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.clock.install();
  await page.route(/\/api\/media\/[12]\/stream$/, route => route.fulfill({
    contentType: 'image/gif', body: Buffer.from('R0lGODlhAQABAAD/ACwAAAAAAQABAAACADs=', 'base64'),
  }));
  await page.goto('/');
  await expect(page.locator('#media-grid')).toBeVisible();
  await page.evaluate(async () => {
    const statePath = '/js/state.js';
    const playbackPath = '/js/playback.js';
    const { state } = await import(statePath);
    const items = [1, 2].map(id => ({ id, type: 'image', file_name: `sample-${id}.gif` }));
    state.media = items;
    (await import(playbackPath)).selectAndPlay(items[0], 0);
  });
  await page.keyboard.press('Shift+S');
  await expect(page.locator('#btn-slideshow')).toHaveAttribute('aria-label', 'Pause slideshow');
  await page.clock.fastForward(5_100);
  await expect(page.locator('#media-image')).toHaveAttribute('src', /\/api\/media\/2\/stream$/);
  await expect.poll(() => page.locator('#media-image').evaluate((image: any) => image.naturalWidth)).toBeGreaterThan(0);
  const imageRequests: string[] = [];
  page.on('request', request => {
    if (/\/api\/media\/[12]\/stream$/.test(request.url())) imageRequests.push(request.url());
  });
  await page.keyboard.press('Escape');
  await expect(page.locator('#player')).not.toHaveClass(/open/);
  expect(await page.evaluate(async () => {
    const imagePath = '/js/imageViewer.js';
    return (await import(imagePath)).isSlideshowActive();
  })).toBe(false);
  await page.clock.fastForward(15_100);
  await expect(page.locator('#player')).not.toHaveClass(/open/);
  expect(imageRequests).toEqual([]);
});
