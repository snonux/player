/**
 * share-single-use.test.ts — a share's max_uses counts viewings, not HTTP
 * requests.
 *
 * A browser fetches one media file with several ranged GETs. When each of
 * them consumed a use, a max_uses: 1 share died after the first chunk
 * (second request: 410, MediaError 2). Now the share page opens one viewing —
 * one use — with POST /s/{token}/view before it touches any media URL; the
 * answer sets a cookie that covers every further request of that browser,
 * including page reloads. Fetching the page itself is free, so link
 * previewers cannot spend the link. These tests play a single-use MP4 share
 * to its end in a real browser, check that another browser is turned away,
 * and that a page whose viewing cannot be opened requests no media.
 */
import { test, expect, type Browser, type BrowserContext, type Page } from '@playwright/test';
import { bootstrap, triggerRescan, waitForServer } from './helpers/server';

let adminCookie = '';

test.beforeAll(async () => {
  test.setTimeout(60_000);
  await waitForServer(15_000);
  adminCookie = await bootstrap();
  await triggerRescan(adminCookie, 30_000);
});

const admin = () => ({ Cookie: adminCookie, 'Content-Type': 'application/json' });

// createSingleUseShare shares the library's MP4 video with max_uses: 1.
async function createSingleUseShare(page: Page): Promise<string> {
  const list = await page.request.get('/api/v1/media?limit=500', { headers: admin() });
  expect(list.ok()).toBeTruthy();
  const video = ((await list.json()) as Array<{ id: number; type: string; file_name: string }>)
    .find(m => m.type === 'video' && m.file_name.endsWith('.mp4'));
  expect(video, 'the test library must contain an MP4 video').toBeTruthy();
  const created = await page.request.post(`/api/v1/media/${video!.id}/shares`, { headers: admin(), data: JSON.stringify({ max_uses: 1 }) });
  expect(created.ok()).toBeTruthy();
  return ((await created.json()) as { token: string }).token;
}

async function usedCount(page: Page, token: string): Promise<number> {
  const res = await page.request.get('/api/v1/shares', { headers: admin() });
  expect(res.ok()).toBeTruthy();
  const share = ((await res.json()) as Array<{ token: string; used_count: number }>).find(s => s.token === token);
  expect(share, 'the share must still exist').toBeTruthy();
  return share!.used_count;
}

// expectPreviewsAreFree fetches the page the way link previewers do and
// checks that the share's uses are untouched and no cookie was handed out.
async function expectPreviewsAreFree(page: Page, browser: Browser, token: string) {
  const previewer = await browser.newContext();
  try {
    for (const userAgent of ['TelegramBot (like TwitterBot)', 'Slackbot-LinkExpanding 1.0', 'Mozilla/5.0 (compatible; Discordbot/2.0)']) {
      const res = await previewer.request.get(`/s/${token}`, { headers: { 'User-Agent': userAgent } });
      expect(res.status(), `page fetch by ${userAgent}`).toBe(200);
    }
    expect((await previewer.cookies()).filter(c => c.name === 'share_view')).toEqual([]);
  } finally {
    await previewer.close();
  }
  expect(await usedCount(page, token)).toBe(0);
}

// shareRequests records, in order, the guest's requests below /s/{token}/
// as "METHOD name" (e.g. "POST view", "GET stream").
function shareRequests(guest: Page, token: string): string[] {
  const seen: string[] = [];
  guest.on('request', req => {
    const path = new URL(req.url()).pathname;
    if (path.startsWith(`/s/${token}/`)) seen.push(`${req.method()} ${path.slice(`/s/${token}/`.length)}`);
  });
  return seen;
}

// openSharePage loads the share page and checks how the viewing starts: the
// page GET hands out nothing; the page's POST brings the credential as a
// cookie scoped to this share (not visible in document.cookie), and the
// page's own URLs carry no credential.
async function openSharePage(context: BrowserContext, guest: Page, token: string) {
  const opened = await guest.goto(`/s/${token}`);
  expect(opened?.status()).toBe(200);
  expect(opened?.headers()['referrer-policy']).toBe('no-referrer');
  expect(opened?.headers()['cache-control']).toBe('no-store');
  expect(opened?.headers()['set-cookie'] ?? '').not.toContain('share_view');

  await expect(guest.locator('#media-video')).toHaveAttribute('src', `/s/${token}/stream`);
  const cookie = (await context.cookies()).find(c => c.name === 'share_view');
  expect(cookie, 'opening the viewing must set the viewing cookie').toBeTruthy();
  expect(cookie).toMatchObject({ path: `/s/${token}`, httpOnly: true, sameSite: 'Lax' });
  expect(await guest.evaluate(() => document.cookie)).not.toContain('share_view');
}

// startPlayback presses play on the (muted) share player.
async function startPlayback(guest: Page) {
  await guest.locator('#media-video').evaluate((el: HTMLVideoElement) => { el.muted = true; });
  await guest.locator('#btn-play').click();
}

// playUntil waits until the video has played past the given position (a
// number of seconds, or 'end': within a second of the end) and returns the
// element's error, '' when there is none.
async function playUntil(guest: Page, upTo: number | 'end'): Promise<string> {
  return guest.evaluate(async upTo => {
    const el = document.getElementById('media-video') as HTMLVideoElement;
    const deadline = Date.now() + 30_000;
    while (Date.now() < deadline) {
      if (el.error) return `MediaError ${el.error.code} ${el.error.message} at t=${el.currentTime}`;
      const target = upTo === 'end' ? el.duration - 1 : upTo;
      if (el.ended || (el.readyState >= 2 && el.currentTime >= target)) return '';
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    return `timeout before ${upTo}: t=${el.currentTime} duration=${el.duration} paused=${el.paused} readyState=${el.readyState}`;
  }, upTo);
}

// playToEnd plays the beginning, then jumps close to the end and plays to
// the end: together with the browser's own probing this needs several ranged
// requests, each of which used to cost a use.
async function playToEnd(guest: Page) {
  await startPlayback(guest);
  expect(await playUntil(guest, 1.5)).toBe('');
  await guest.locator('#media-video').evaluate((el: HTMLVideoElement) => { el.currentTime = el.duration - 4; });
  expect(await playUntil(guest, 'end')).toBe('');
  expect(await guest.locator('#media-video').evaluate((el: HTMLVideoElement) => el.error)).toBeNull();
  await expect(guest.locator('#toast')).not.toHaveClass(/error/);
}

// expectSpentForOthers checks that a browser without the viewing is refused.
async function expectSpentForOthers(browser: Browser, token: string) {
  const stranger = await browser.newContext();
  try {
    for (const path of [`/s/${token}`, `/s/${token}/stream`, `/s/${token}/download`]) {
      expect((await stranger.request.get(path)).status(), `${path} for another browser`).toBe(410);
    }
    expect((await stranger.request.post(`/s/${token}/view`)).status(), 'opening a viewing in another browser').toBe(410);
  } finally {
    await stranger.close();
  }
}

test('a max_uses=1 share plays an MP4 to the end, survives a reload, and is spent for everyone else', async ({ page, browser }) => {
  test.setTimeout(90_000);
  const token = await createSingleUseShare(page);
  await expectPreviewsAreFree(page, browser, token);
  const anonymous = await browser.newContext();
  try {
    const guest = await anonymous.newPage();
    const requests = shareRequests(guest, token);
    const mediaStatuses: number[] = [];
    guest.on('response', res => {
      if (new URL(res.url()).pathname === `/s/${token}/stream`) mediaStatuses.push(res.status());
    });
    await openSharePage(anonymous, guest, token);
    // The viewing is opened before anything else of the share is requested.
    expect(requests[0]).toBe('POST view');
    expect(requests.filter(r => r === 'POST view')).toHaveLength(1);
    expect(await usedCount(page, token)).toBe(1);

    // A Chromium build without the proprietary codecs cannot play the MP4
    // at all; that says nothing about shares, so do not report a failure.
    const decodable = await guest.evaluate(() => document.createElement('video').canPlayType('video/mp4; codecs="avc1.42E01E"'));
    test.skip(decodable === '', 'this browser build has no H.264 decoder');

    await playToEnd(guest);
    expect(mediaStatuses.length, 'playback must have needed more than one media request').toBeGreaterThan(1);
    expect(mediaStatuses.filter(s => s !== 200 && s !== 206), 'no media request may be refused').toEqual([]);
    expect(await usedCount(page, token)).toBe(1);

    // Reloading inside the same browser is the same viewing: the page's
    // POST is sent again, with the cookie, and consumes nothing.
    expect((await guest.reload())?.status()).toBe(200);
    await expect(guest.locator('#media-video')).toHaveAttribute('src', `/s/${token}/stream`);
    await startPlayback(guest);
    expect(await playUntil(guest, 1)).toBe('');
    expect(requests.filter(r => r === 'POST view')).toHaveLength(2);

    await expectSpentForOthers(browser, token);
    expect(await usedCount(page, token)).toBe(1);
  } finally {
    await anonymous.close();
    await page.request.delete(`/api/v1/shares/${token}`, { headers: admin() });
  }
});

// The page was loaded while the share was usable, but the viewing cannot be
// opened any more (here: the server answers the POST with 410, as it does
// when another visitor took the last use in between). The page must say so
// and must not request any media: each such request would otherwise try to
// open a viewing of its own.
test('a share page whose viewing cannot be opened shows the invalid state and requests no media', async ({ page, browser }) => {
  const token = await createSingleUseShare(page);
  const anonymous = await browser.newContext();
  try {
    const guest = await anonymous.newPage();
    const requests = shareRequests(guest, token);
    await guest.route(`**/s/${token}/view`, route => route.fulfill({ status: 410, body: 'gone' }));

    expect((await guest.goto(`/s/${token}`))?.status()).toBe(200);
    await expect(guest.locator('#share-status')).toHaveText('This share link is no longer valid.');
    await expect(guest.locator('#player')).toBeHidden();
    // Give a stray media request time to show up before asserting absence.
    await guest.waitForTimeout(500);
    expect(requests).toEqual(['POST view']);
    for (const id of ['media-video', 'media-audio', 'cover-art']) {
      expect(await guest.locator(`#${id}`).getAttribute('src'), `${id} must have no source`).toBeNull();
    }
    expect(await usedCount(page, token)).toBe(0);
  } finally {
    await anonymous.close();
    await page.request.delete(`/api/v1/shares/${token}`, { headers: admin() });
  }
});
