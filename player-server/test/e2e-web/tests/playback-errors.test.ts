/**
 * playback-errors.test.ts — the player's compat-stream "preparing" state, its
 * error toast and the retry on the next play press, in a real browser. The
 * compat endpoint is faked with page.route, so the test needs neither ffmpeg
 * nor a transcoding server.
 */
import { test, expect, type Page, type Route } from '@playwright/test';
import { bootstrap, waitForServer } from './helpers/server';

test.use({ serviceWorkers: 'block' });

const COMPAT_URL = '/api/media/987654/compat';
const GARBAGE = Buffer.alloc(32 * 1024, 0x5a);

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

// The server's "still preparing" answer to the HEAD probe: no body, the
// state travels in Retry-After and X-Transcode-Status.
function preparing(route: Route, status: 'transcoding' | 'busy') {
  return route.fulfill({ status: 503, headers: { 'Retry-After': '1', 'X-Transcode-Status': status } });
}

const AVI = { id: 987654, type: 'video', file_name: 'clip.avi', duration: 5, transcoded: true };

test('a compat stream shows the preparing state until the server is ready, then loads', async ({ page }) => {
  const methods: string[] = [];
  await page.route(`**${COMPAT_URL}`, route => {
    const method = route.request().method();
    methods.push(method);
    const probes = methods.filter(m => m === 'HEAD').length;
    if (method === 'HEAD' && probes <= 2) return preparing(route, probes === 1 ? 'transcoding' : 'busy');
    if (method === 'HEAD') return route.fulfill({ status: 200, contentType: 'video/mp4' });
    // Ready, but the "rendition" is garbage: the element must report it.
    return route.fulfill({ status: 200, contentType: 'video/mp4', body: GARBAGE });
  });

  await loadDirect(page, AVI, COMPAT_URL);

  const status = page.locator('#player-status');
  await expect(status).toHaveText('Preparing clip.avi for playback…');
  await expect(status).toBeVisible();
  expect(await page.locator('#media-video').getAttribute('src')).toBeNull();
  await expect(page.locator('#toast')).toHaveText('Preparing clip.avi for playback…');
  await expect(status).toHaveText('Server is busy, clip.avi is waiting to be prepared…');
  // Only HEAD probes so far: a GET would consume a use of a share link.
  expect(methods.every(m => m === 'HEAD')).toBe(true);

  // Third probe answers 200: the URL reaches the element and the status goes.
  await expect(page.locator('#media-video')).toHaveAttribute('src', COMPAT_URL, { timeout: 10_000 });
  await expect(status).toBeHidden();
  await expect(page.locator('#toast')).toContainText('Cannot play clip.avi:', { timeout: 10_000 });
  await expect(page.locator('#toast')).toHaveClass(/error/);
  await expect(page.locator('#btn-play')).toHaveText('▶');
});

test('the big play prompt stays clickable while preparing and queues playback', async ({ page }) => {
  await page.route(`**${COMPAT_URL}`, route => preparing(route, 'transcoding'));
  await loadDirect(page, AVI, COMPAT_URL);
  await expect(page.locator('#player-status')).toBeVisible();
  // A direct load waits for a play press: prompt visible, not covered.
  await expect(page.locator('#btn-play')).toHaveText('▶');
  await page.locator('#big-play').click({ timeout: 5_000 });
  await expect(page.locator('#btn-play')).toHaveText('⏸');
  await expect(page.locator('#big-play')).toBeHidden();
  expect(await page.evaluate(async () => {
    const modulePath = '/js/playback.js';
    return (await import(modulePath)).hasPendingPlay();
  })).toBe(true);
});

test('a failed transcode shows the error toast, and pressing play retries', async ({ page }) => {
  let fail = true;
  let probes = 0;
  await page.route(`**${COMPAT_URL}`, route => {
    if (route.request().method() !== 'HEAD') return route.fulfill({ status: 200, contentType: 'audio/mp4', body: GARBAGE });
    probes += 1;
    return route.fulfill({ status: fail ? 500 : 200 });
  });

  await loadDirect(page, { id: 987654, type: 'audio', file_name: 'song.wma', duration: 5, transcoded: true }, COMPAT_URL);

  await expect(page.locator('#toast')).toHaveText('Cannot play song.wma: the server could not convert this file');
  await expect(page.locator('#toast')).toHaveClass(/error/);
  await expect(page.locator('#btn-play')).toHaveText('▶');
  await expect(page.locator('#player-status')).toBeHidden();
  expect(await page.locator('#media-audio').getAttribute('src')).toBeNull();
  expect(probes).toBe(1);

  // The play button is not dead after a failure: it loads the item again.
  fail = false;
  await page.locator('#btn-play').click();
  await expect(page.locator('#media-audio')).toHaveAttribute('src', COMPAT_URL);
  expect(probes).toBe(2);
});

test('switching to another item while preparing cancels the pending stream', async ({ page }) => {
  await page.route(`**${COMPAT_URL}`, route => preparing(route, 'transcoding'));

  await loadDirect(page, AVI, COMPAT_URL);
  await expect(page.locator('#player-status')).toHaveText('Preparing clip.avi for playback…');

  const image = 'data:image/gif;base64,R0lGODlhAQABAAD/ACwAAAAAAQABAAACADs=';
  await loadDirect(page, { id: 2, type: 'image', file_name: 'next.gif' }, image);
  await expect(page.locator('#player-status')).toBeHidden();
  await page.unroute(`**${COMPAT_URL}`);
  await page.waitForTimeout(1500);
  expect(await page.locator('#media-video').getAttribute('src')).not.toBe(COMPAT_URL);
  await expect(page.locator('#player')).toHaveClass(/has-image/);
});
