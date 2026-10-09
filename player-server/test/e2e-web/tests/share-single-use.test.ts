/**
 * share-single-use.test.ts — a share's max_uses counts viewings, not HTTP
 * requests.
 *
 * A browser fetches one media file with several ranged GETs. When each of
 * them consumed a use, a max_uses: 1 share died after the first chunk
 * (second request: 410, MediaError 2). Now loading the share page opens one
 * viewing — one use — and sets an HttpOnly cookie that covers every further
 * request of that browser, including page reloads. This test plays a
 * single-use MP4 share to its end in a real browser and checks that another
 * browser is turned away.
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

// openSharePage loads the share page and checks how the viewing credential
// arrives: as a cookie scoped to this share that page scripts cannot read,
// while the page's own URLs carry no credential.
async function openSharePage(context: BrowserContext, guest: Page, token: string) {
  const opened = await guest.goto(`/s/${token}`);
  expect(opened?.status()).toBe(200);
  expect(opened?.headers()['referrer-policy']).toBe('no-referrer');
  expect(opened?.headers()['cache-control']).toBe('no-store');

  const cookie = (await context.cookies()).find(c => c.name === 'share_view');
  expect(cookie, 'the share page must set the viewing cookie').toBeTruthy();
  expect(cookie).toMatchObject({ path: `/s/${token}`, httpOnly: true, sameSite: 'Lax' });
  expect(await guest.evaluate(() => document.cookie)).not.toContain('share_view');
  await expect(guest.locator('#media-video')).toHaveAttribute('src', `/s/${token}/stream`);
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

// expectSpentForOthers checks that a browser without the viewing is refused.
async function expectSpentForOthers(browser: Browser, token: string) {
  const stranger = await browser.newContext();
  try {
    for (const path of [`/s/${token}`, `/s/${token}/stream`, `/s/${token}/download`]) {
      expect((await stranger.request.get(path)).status(), `${path} for another browser`).toBe(410);
    }
  } finally {
    await stranger.close();
  }
}

test('a max_uses=1 share plays an MP4 to the end, survives a reload, and is spent for everyone else', async ({ page, browser }) => {
  test.setTimeout(90_000);
  const token = await createSingleUseShare(page);
  const anonymous = await browser.newContext();
  try {
    const guest = await anonymous.newPage();
    const mediaStatuses: number[] = [];
    guest.on('response', res => {
      if (new URL(res.url()).pathname === `/s/${token}/stream`) mediaStatuses.push(res.status());
    });
    await openSharePage(anonymous, guest, token);

    // A Chromium build without the proprietary codecs cannot play the MP4
    // at all; that says nothing about shares, so do not report a failure.
    const decodable = await guest.evaluate(() => document.createElement('video').canPlayType('video/mp4; codecs="avc1.42E01E"'));
    test.skip(decodable === '', 'this browser build has no H.264 decoder');

    // Play the beginning, then jump close to the end: together with the
    // browser's own probing this needs several ranged requests, each of
    // which used to cost a use.
    await startPlayback(guest);
    expect(await playUntil(guest, 1.5)).toBe('');
    await guest.locator('#media-video').evaluate((el: HTMLVideoElement) => { el.currentTime = el.duration - 4; });
    expect(await playUntil(guest, 'end')).toBe('');
    expect(await guest.locator('#media-video').evaluate((el: HTMLVideoElement) => el.error)).toBeNull();
    await expect(guest.locator('#toast')).not.toHaveClass(/error/);

    expect(mediaStatuses.length, 'playback must have needed more than one media request').toBeGreaterThan(1);
    expect(mediaStatuses.filter(s => s !== 200 && s !== 206), 'no media request may be refused').toEqual([]);
    expect(await usedCount(page, token)).toBe(1);

    // Reloading inside the same browser is the same viewing.
    expect((await guest.reload())?.status()).toBe(200);
    await startPlayback(guest);
    expect(await playUntil(guest, 1)).toBe('');

    await expectSpentForOthers(browser, token);
    expect(await usedCount(page, token)).toBe(1);
  } finally {
    await anonymous.close();
    await page.request.delete(`/api/v1/shares/${token}`, { headers: admin() });
  }
});
