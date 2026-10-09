/**
 * playback-errors.test.ts — the player's compat-stream "preparing" state and
 * its error toast, in a real browser. The compat endpoint is faked with
 * page.route, so the test needs neither ffmpeg nor a transcoding server.
 */
import { test, expect, type Page } from '@playwright/test';
import { bootstrap, waitForServer } from './helpers/server';

test.use({ serviceWorkers: 'block' });

const COMPAT_URL = '/api/media/987654/compat';

let adminCookie: string;

test.beforeAll(async () => {
  await waitForServer();
  adminCookie = await bootstrap();
});

test.beforeEach(async ({ page }) => {
  const origin = new URL(process.env.PLAYER_URL || 'http://localhost:8080');
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.goto('/');
});

// Loads a media object the way the share page and the detached popup do.
async function loadDirect(page: Page, media: Record<string, unknown>, url: string) {
  await page.evaluate(async args => {
    const modulePath = '/js/playback.js';
    const { loadMediaDirect } = await import(modulePath);
    loadMediaDirect(args.media, args.url, '');
  }, { media, url });
}

test('a compat stream shows the preparing state until the server is ready, then loads', async ({ page }) => {
  let probes = 0;
  await page.route(`**${COMPAT_URL}`, route => {
    probes += 1;
    if (probes <= 2) {
      return route.fulfill({
        status: 503,
        headers: { 'Retry-After': '1' },
        contentType: 'application/json',
        body: JSON.stringify({ error: 'transcode in progress', retry_after_seconds: 1, status: probes === 1 ? 'transcoding' : 'busy' }),
      });
    }
    // Ready, but the "rendition" is garbage: the element must report it.
    return route.fulfill({ status: 200, contentType: 'video/mp4', body: Buffer.alloc(32 * 1024, 0x5a) });
  });

  await loadDirect(page, { id: 987654, type: 'video', file_name: 'clip.avi', duration: 5, transcoded: true }, COMPAT_URL);

  const status = page.locator('#player-status');
  await expect(status).toHaveText('Preparing clip.avi for playback…');
  await expect(status).toBeVisible();
  expect(await page.locator('#media-video').getAttribute('src')).toBeNull();
  await expect(page.locator('#toast')).toHaveText('Preparing clip.avi for playback…');
  await expect(status).toHaveText('Server is busy, clip.avi is waiting to be prepared…');

  // Third probe answers 200: the URL reaches the element and the status goes.
  await expect(page.locator('#media-video')).toHaveAttribute('src', COMPAT_URL, { timeout: 10_000 });
  await expect(status).toBeHidden();
  await expect(page.locator('#toast')).toContainText('Cannot play clip.avi:', { timeout: 10_000 });
  await expect(page.locator('#toast')).toHaveClass(/error/);
  await expect(page.locator('#btn-play')).toHaveText('▶');
});

test('a failed transcode shows the error toast and never loads the element', async ({ page }) => {
  await page.route(`**${COMPAT_URL}`, route => route.fulfill({
    status: 500,
    contentType: 'application/json',
    body: JSON.stringify({ error: 'transcode failed' }),
  }));

  await loadDirect(page, { id: 987654, type: 'audio', file_name: 'song.wma', duration: 5, transcoded: true }, COMPAT_URL);

  await expect(page.locator('#toast')).toHaveText('Cannot play song.wma: the server could not convert this file');
  await expect(page.locator('#toast')).toHaveClass(/error/);
  await expect(page.locator('#btn-play')).toHaveText('▶');
  await expect(page.locator('#player-status')).toBeHidden();
  expect(await page.locator('#media-audio').getAttribute('src')).toBeNull();
});

test('switching to another item while preparing cancels the pending stream', async ({ page }) => {
  await page.route(`**${COMPAT_URL}`, route => route.fulfill({
    status: 503,
    headers: { 'Retry-After': '1' },
    contentType: 'application/json',
    body: JSON.stringify({ error: 'transcode in progress', retry_after_seconds: 1, status: 'transcoding' }),
  }));

  await loadDirect(page, { id: 987654, type: 'video', file_name: 'clip.avi', duration: 5, transcoded: true }, COMPAT_URL);
  await expect(page.locator('#player-status')).toHaveText('Preparing clip.avi for playback…');

  const image = 'data:image/gif;base64,R0lGODlhAQABAAD/ACwAAAAAAQABAAACADs=';
  await loadDirect(page, { id: 2, type: 'image', file_name: 'next.gif' }, image);
  await expect(page.locator('#player-status')).toBeHidden();
  await page.unroute(`**${COMPAT_URL}`);
  await page.waitForTimeout(1500);
  expect(await page.locator('#media-video').getAttribute('src')).not.toBe(COMPAT_URL);
  await expect(page.locator('#player')).toHaveClass(/has-image/);
});
