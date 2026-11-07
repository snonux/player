// Disposable podcast_fixture.py beta feed: first=60s, second/third=12s.
import { readFileSync } from 'node:fs';
import { test, expect, type Page } from '@playwright/test';
import { keyboardLogin } from './helpers/music-ui';
const directory = process.env.PLAYER_PODCAST_FIXTURE;
test.skip(!directory, 'Requires the disposable podcast fixture');
const auth = directory ? JSON.parse(readFileSync(`${directory}/auth.json`, 'utf8')) : {};
async function open(page: Page) {
  await keyboardLogin(page, auth.admin);
  const all = page.getByRole('button', { name: 'All podcast episodes', exact: true });
  if (!await all.isVisible()) await page.getByRole('button', { name: 'Toggle sets sidebar', exact: true }).click();
  await all.click();
  await expect(page.locator('.episode-card').filter({ hasText: 'Beta Episode 002' })).toBeVisible();
}
async function fresh(page: Page, title: string) {
  const card = page.locator('.episode-card').filter({ hasText: title });
  await card.locator('.card-more').click();
  const uncomplete = page.getByRole('button', { name: 'Mark uncompleted', exact: true });
  if (await uncomplete.isVisible()) await uncomplete.click();
  else await page.keyboard.press('Escape');
}
async function play(page: Page) {
  const card = page.locator('.episode-card').filter({ hasText: 'Beta Episode 002' });
  await card.getByRole('button', { name: /^(Download and play|Play|Resume) episode$/ }).click();
  await expect.poll(() => page.locator('#media-audio').evaluate((a: HTMLAudioElement) => !a.paused && a.currentTime > 0.2)).toBe(true);
}
test('natural completion starts the next episode and the feed stops without wrapping', async ({ page }) => {
  await open(page); await fresh(page, 'Beta Episode 002'); await fresh(page, 'Beta Episode 003'); await play(page);
  await expect(page.locator('#player')).toContainText('Beta Episode 003', { timeout: 30_000 });
  await expect.poll(() => page.locator('#media-audio').evaluate((a: HTMLAudioElement) => a.ended), { timeout: 25_000 }).toBe(true);
  await page.waitForTimeout(500);
  await expect(page.locator('#player')).toContainText('Beta Episode 003');
});
test('paused seeking to end never starts the next episode', async ({ page }) => {
  await open(page); await fresh(page, 'Beta Episode 002'); await play(page);
  await page.locator('#btn-play').click();
  await page.locator('#media-audio').evaluate((a: HTMLAudioElement) => { a.currentTime = a.duration; });
  await page.waitForTimeout(1000);
  await expect(page.locator('#player')).toContainText('Beta Episode 002');
  expect(await page.locator('#media-audio').evaluate((a: HTMLAudioElement) => a.paused)).toBe(true);
});
test('a failed successor stops without skipping or wrapping', async ({ page }) => {
  await open(page); await fresh(page, 'Beta Episode 002'); await fresh(page, 'Beta Episode 003');
  const response = await page.request.get('/api/podcasts/episodes?filter=all&limit=200');
  const rows = await response.json();
  const next = rows.find((row: any) => row.title === 'Beta Episode 003');
  expect(next.server_copy.media_id).toBeTruthy();
  await page.route(new RegExp(`/api/(?:v1/)?media/${next.server_copy.media_id}/(?:stream|compat)`), route => route.fulfill({ status: 500, body: 'Fixture stream unavailable' }));
  await play(page);
  await expect(page.locator('#player')).toContainText('Beta Episode 003', { timeout: 30_000 });
  await expect.poll(() => page.locator('#media-audio').evaluate((a: HTMLAudioElement) => Boolean(a.error)), { timeout: 15_000 }).toBe(true);
  await page.waitForTimeout(500);
  await expect(page.locator('#player')).toContainText('Beta Episode 003');
  expect(await page.locator('#media-audio').evaluate((a: HTMLAudioElement) => a.paused)).toBe(true);
});

for (const phase of ['lookup', 'download', 'preparation']) {
  for (const command of ['seek', 'pause']) {
    test(`${command} cancels pending continuation during ${phase}`, async ({ page }) => {
      await open(page); await fresh(page, 'Beta Episode 002'); await fresh(page, 'Beta Episode 003');
      const catalog = await (await page.request.get('/api/podcasts/episodes?filter=all&limit=200')).json();
      const next = catalog.find((row: any) => row.title === 'Beta Episode 003');
      const id = next.server_copy.media_id;
      const nextMedia = (await (await page.request.get(`/api/media/${id}`)).json()).media;
      let release!: () => void;
      const gate = new Promise<void>(resolve => { release = resolve; });
      let reached = false, details = 0, nextStreams = 0;
      page.on('request', request => {
        if (new RegExp(`/api/(?:v1/)?media/${id}/(?:stream|compat)`).test(request.url())) nextStreams++;
      });
      await page.route('**/api/podcasts/episodes?**', async route => {
        const response = await route.fetch();
        const rows = await response.json();
        if (!new URL(route.request().url()).searchParams.has('feed_key')) return route.fulfill({ response });
        if (phase === 'lookup') { reached = true; await gate; }
        if (phase === 'download') {
          for (const row of rows) if (row.episode_key === next.episode_key) row.server_copy = null;
        }
        await route.fulfill({ response, json: rows });
      });
      if (phase === 'download') {
        await page.route(`**/api/podcasts/episodes/${next.id}/download`, async route => {
          reached = true; await gate;
          await route.fulfill({ json: nextMedia });
        });
      }
      if (phase === 'preparation') {
        await page.route(`**/api/media/${id}`, async route => {
          const response = await route.fetch();
          if (++details === 2) { reached = true; await gate; }
          await route.fulfill({ response });
        });
      }
      try {
        await play(page);
        await page.locator('#media-audio').evaluate((audio: HTMLAudioElement) => { audio.playbackRate = 8; });
        await expect.poll(() => reached, { timeout: 20_000 }).toBe(true);
        if (command === 'seek') {
          await page.locator('#media-audio').evaluate(async (audio: HTMLAudioElement) => {
            const sought = new Promise<void>(resolve => audio.addEventListener('seeked', () => resolve(), { once: true }));
            audio.currentTime = 1;
            await sought;
          });
        } else {
          // Same entry point used by the OS/media-session Pause action. At an
          // ended element this deliberately produces no native pause event.
          await page.evaluate(async () => { (await import('/js/player.js')).pausePlayback(); });
        }
        release();
        await page.waitForTimeout(1000);
        await expect(page.locator('#player')).toContainText('Beta Episode 002');
        expect(nextStreams).toBe(0);
        expect(await page.locator('#media-audio').evaluate((audio: HTMLAudioElement) => audio.paused)).toBe(true);
      } finally { release(); }
    });
  }
}
