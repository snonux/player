import { readFileSync } from 'node:fs';
import { test, expect, type Page } from '@playwright/test';

// Use the disposable all_formats_fixture.py server, never a private library.
const authPath = process.env.PLAYER_FORMAT_AUTH;
test.skip(!authPath, 'start all_formats_fixture.py and set PLAYER_FORMAT_AUTH');
const auth = authPath ? JSON.parse(readFileSync(authPath, 'utf8')) : {};

async function expectPlayback(page: Page, after: number) {
  const video = page.locator('#media-video');
  await expect.poll(() => video.evaluate((el: HTMLVideoElement) => {
    if (el.error) throw new Error(`MediaError ${el.error.code}: ${el.error.message}`);
    return el.currentTime;
  }), { timeout: 60_000 }).toBeGreaterThan(after);
  expect(await video.evaluate((el: HTMLVideoElement) => el.videoWidth)).toBeGreaterThan(0);
}

async function seekWhilePaused(page: Page, seconds: number) {
  const video = page.locator('#media-video');
  await video.evaluate((el: HTMLVideoElement) => el.pause());
  await video.evaluate((el: HTMLVideoElement, seconds) => { el.currentTime = seconds; }, seconds);
  await expect.poll(() => video.evaluate((el: HTMLVideoElement, target) =>
    el.paused && !el.seeking && Math.abs(el.currentTime - target) < 0.4,
  seconds), { timeout: 10_000 }).toBe(true);
  await video.evaluate((el: HTMLVideoElement) => el.play());
}

for (const ext of ['mpg', 'mpeg']) {
  test(`.${ext} scans, plays and seeks through compatibility streams and public shares`, async ({ page, browser }) => {
    test.setTimeout(120_000);
    await page.goto('/login.html');
    await page.locator('#username').fill(auth.username);
    await page.locator('#password').fill(auth.password);
    await page.locator('button[type="submit"]').click();
    await page.waitForURL(url => url.pathname === '/');
    const name = `sample-${ext}.${ext}`;
    const media = await (await page.request.get('/api/v1/media?limit=100')).json();
    const item = media.find((m: { file_name: string }) => m.file_name === name);
    expect(item).toMatchObject({ type: 'video', transcoded: true });
    expect(item.codec).toBe(`${ext === 'mpg' ? 'mpeg1video' : 'mpeg2video'}/mp2`);
    const id = item.id;
    const hint = await (await page.request.get(`/api/v1/media/${id}/playback`)).json();
    expect(hint).toMatchObject({ transcoded: true, playback_url: `/api/v1/media/${id}/compat` });

    await page.locator('.set-card').filter({ hasText: 'test-videos' }).locator('.title').click();
    const card = page.locator(`.media-card[aria-label="${name}"]`);
    await card.hover();
    await card.locator('[data-action="play"]').click();
    await expect(page.locator('#media-video')).toHaveAttribute('src', `/api/media/${id}/compat`);
    await expectPlayback(page, 1);
    await seekWhilePaused(page, 4);
    await expectPlayback(page, 4.5);
    const ranged = await page.request.get(`/api/v1/media/${id}/compat`, { headers: { Range: 'bytes=0-31' } });
    expect(ranged.status()).toBe(206);
    expect(ranged.headers()['content-type']).toBe('video/mp4');
    expect((await ranged.body()).length).toBe(32);
    const original = await page.request.get(`/api/v1/media/${id}/download`, { headers: { Range: 'bytes=0-31' } });
    expect(original.status()).toBe(206);
    expect(original.headers()['content-type']).toBe('video/mpeg');
    expect(await original.body()).not.toEqual(await ranged.body());

    const created = await page.request.post(`/api/v1/media/${id}/shares`, { data: { max_uses: 1 } });
    expect(created.ok()).toBeTruthy();
    const { token } = await created.json();
    const guest = await browser.newContext();
    try {
      expect((await guest.request.get(`/api/v1/media/${id}/compat`)).status()).toBe(401);
      const shared = await guest.newPage();
      await shared.goto(`/s/${token}`);
      await expect(shared.locator('body')).toHaveAttribute('data-share-viewing', 'open');
      await expect(shared.locator('#media-video')).toHaveAttribute('src', `/s/${token}/compat`);
      await shared.locator('#media-video').evaluate((el: HTMLVideoElement) => { el.muted = true; });
      await shared.locator('#btn-play').click();
      await expectPlayback(shared, 1);
      await seekWhilePaused(shared, 4);
      await expectPlayback(shared, 4.5);
      const shares = await (await page.request.get('/api/v1/shares')).json();
      expect(shares.find((s: { token: string }) => s.token === token).used_count).toBe(1);
    } finally {
      await guest.close();
      await page.request.delete(`/api/v1/shares/${token}`);
    }
  });
}
