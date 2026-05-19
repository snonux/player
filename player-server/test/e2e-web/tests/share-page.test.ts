/**
 * share-page.test.ts — Playwright tests for the public share page served at /s/{token}.
 *
 * Coverage:
 *   1. Create a share via API, navigate to /s/{token} → page loads (HTTP 200) and
 *      the shared media title is reflected somewhere in the rendered HTML.
 *   2. The shared media playback element exists in the page (the share page uses
 *      a combined <video>+<audio> stage; for an audio share the <audio> element
 *      receives the source and the <video> is hidden).
 *   3. The page loads with no JS console errors.
 *   4. Navigating to /s/{invalid-token} returns a 404 (not-found) response.
 *
 * The share page is fully public — it does not require a session cookie. We do
 * still need an admin session to create a share via /api/v1/media/{id}/shares,
 * so the file mirrors smoke.test.ts and runs bootstrap + rescan in beforeAll.
 *
 * Note on feature gaps:
 *   - The current share.html does not render the original file name in the title
 *     bar — the <title> is the static "Shared Media" string and the file name is
 *     only embedded inside the JSON payload at #share-meta. We therefore assert
 *     against the static title and the JSON payload, not against a visible label.
 */

import { test, expect, Page, BrowserContext } from '@playwright/test';
import {
  bootstrap,
  triggerRescan,
  waitForServer,
} from './helpers/server';

const BASE_URL = process.env.PLAYER_URL || 'http://localhost:8080';

let adminCookie: string = '';

// Allow 60 s for beforeAll: the rescan may take 30+ seconds on a large library.
test.beforeAll(async () => {
  await waitForServer(15_000);
  adminCookie = await bootstrap();
  await triggerRescan(adminCookie, 30_000);
}, 60_000);

/**
 * createShareForFirstMedia uses the admin session to find the first available
 * media item and create a share for it. Returns the share token.
 *
 * The helper centralises the share-creation dance so each test does not need
 * to repeat the API plumbing.
 */
async function createShareForFirstMedia(page: Page): Promise<string> {
  // GET /api/v1/media?limit=1 returns the first available media item from any set.
  const mediaRes = await page.request.get('/api/v1/media?limit=1', {
    headers: { Cookie: adminCookie },
  });
  expect(mediaRes.ok()).toBeTruthy();
  const mediaList = (await mediaRes.json()) as Array<{ id: number }> | null;
  if (!mediaList || mediaList.length === 0) {
    throw new Error('No media available — cannot create share fixture');
  }

  const mediaId = mediaList[0].id;
  // POST creates a fresh share token; the server uses ShareDefaultExpiryDays
  // for the expiry — we do not need to specify a body field for that.
  const shareRes = await page.request.post(`/api/v1/media/${mediaId}/shares`, {
    headers: { Cookie: adminCookie, 'Content-Type': 'application/json' },
    data: JSON.stringify({}),
  });
  expect(shareRes.ok()).toBeTruthy();
  const share = (await shareRes.json()) as { token: string };
  return share.token;
}

// -----------------------------------------------------------------------
// 1. /s/{token} page loads with HTTP 200 and serves the share.html shell.
// -----------------------------------------------------------------------

test('share page loads and exposes the share metadata payload', async ({ page }) => {
  const token = await createShareForFirstMedia(page);

  // Capture console errors so we can assert the page is clean (test 3).
  const consoleErrors: string[] = [];
  page.on('pageerror', (err) => consoleErrors.push(`pageerror: ${err.message}`));
  page.on('console', (msg) => {
    if (msg.type() === 'error') consoleErrors.push(`console.error: ${msg.text()}`);
  });

  // Navigate to the share page (no cookie required — public route).
  const response = await page.goto(`/s/${token}`);
  expect(response?.status()).toBe(200);

  // The static title of share.html is "Shared Media".
  await expect(page).toHaveTitle('Shared Media');

  // The <h1> heading in share.html reads "Shared Media".
  await expect(page.locator('h1')).toHaveText('Shared Media');

  // The share metadata is embedded as a JSON script tag — confirm it parsed.
  // The handler replaces the placeholder <!--SHARE_MEDIA--> with a JSON object
  // containing media + stream_url. We check the tag exists and that its text
  // is valid JSON referencing the media id we created.
  const metaText = await page.locator('#share-meta').textContent();
  expect(metaText, 'share-meta script tag should contain JSON payload').toBeTruthy();
  const meta = JSON.parse(metaText || '{}') as { media?: { id: number; type: string }; stream_url?: string };
  expect(meta.media?.id).toBeGreaterThan(0);
  expect(meta.stream_url).toContain(token);

  // No JS console errors should have fired while the page initialised.
  // We allow a brief settle to let initPlayer() finish wiring the audio source.
  await page.waitForTimeout(500);
  expect(consoleErrors, `unexpected console errors: ${consoleErrors.join(' | ')}`).toEqual([]);
});

// -----------------------------------------------------------------------
// 2. The share page renders an <audio> or <video> element that the player can
//    attach a source to. share.html has both elements in the DOM at all times
//    — initPlayer() reveals the correct one for the media type.
// -----------------------------------------------------------------------

test('share page contains audio and video stage elements', async ({ page }) => {
  const token = await createShareForFirstMedia(page);
  await page.goto(`/s/${token}`);

  // Both stage elements must exist in the DOM (one of them is hidden until
  // initPlayer() chooses it based on media type). We assert presence in the
  // DOM rather than visibility to stay agnostic to the media type returned
  // by /api/v1/media?limit=1 (audiobooks vs. videos vs. images).
  const audio = page.locator('audio#media-audio');
  const video = page.locator('video#media-video');
  await expect(audio).toHaveCount(1);
  await expect(video).toHaveCount(1);

  // The download button is present even when the share has no explicit
  // download_url — initPlayer() may hide it via JS, but the DOM element exists.
  const downloadBtn = page.locator('#btn-download');
  await expect(downloadBtn).toHaveCount(1);
});

// -----------------------------------------------------------------------
// 3. /s/{invalid-token} returns a 404 (the handler explicitly writes
//    http.StatusNotFound when shareSvc.GetSharedMedia returns nil/error).
// -----------------------------------------------------------------------

test('invalid share token returns 404', async ({ page }) => {
  // Make a raw request so we get the status code without following redirects.
  const response = await page.request.get('/s/this-token-does-not-exist', {
    maxRedirects: 0,
  });
  expect(response.status()).toBe(404);
});

// -----------------------------------------------------------------------
// Bonus: revoked / nonexistent token via page.goto() shows the server's
// default 404 body (no SPA shell). We verify the page does not contain the
// share.html marker.
// -----------------------------------------------------------------------

test('navigating to invalid share token in a browser does not load share UI', async ({ page }) => {
  const response = await page.goto('/s/another-bad-token', { waitUntil: 'load' });
  expect(response?.status()).toBe(404);
  // share.html exposes a <h1>Shared Media</h1>; the 404 plain-text body does not.
  await expect(page.locator('h1', { hasText: 'Shared Media' })).toHaveCount(0);
});

// Silence unused-import warning when BrowserContext is only referenced for typings.
export type _Unused = BrowserContext;
