import { test, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';

// This suite only signs in and writes device-local browser data. The optional
// credentials file belongs to a disposable localhost server, never production.
const auth = process.env.PLAYER_DEVICE_LIBRARY_AUTH
  ? JSON.parse(readFileSync(process.env.PLAYER_DEVICE_LIBRARY_AUTH, 'utf8')) : null;

test('old offline music URLs require normal sign-in; device entries remain', async ({ page }) => {
  for (const entry of ['/?offline=music', '/index.html?offline=music']) {
    await page.goto(entry);
    await expect(page).toHaveURL(/\/login\.html\?/);
    await expect(page.getByRole('link', { name: 'Browse offline music' })).toHaveCount(0);
    await expect(page.getByRole('heading', { name: 'Offline music', exact: true })).toHaveCount(0);
  }
  await page.getByRole('link', { name: 'On this device', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'On this device', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Add files', exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Podcasts', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Podcasts on this device', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Add feed URL', exact: true })).toBeVisible();
});

test('signed-in legacy URLs load online library and retain download settings and bytes', async ({ page }) => {
  test.skip(!auth, 'Set PLAYER_DEVICE_LIBRARY_AUTH to disposable localhost credentials');
  await page.goto('/login.html');
  await page.evaluate(() => {
    localStorage.setItem('player-offline-v1-999:index', '{"sentinel":true}');
  });
  await page.evaluate(async () => {
    await (await caches.open('player-offline-v1-999')).put('/saved-sentinel', new Response('saved bytes'));
  });
  expect((await page.request.post('/api/login', { data: auth })).ok()).toBe(true);
  for (const entry of ['/?offline=music', '/index.html?offline=music']) {
    const online = page.waitForResponse(response => new URL(response.url()).pathname === '/api/sets' && response.ok());
    await page.goto(entry); await online;
    await expect(page.getByRole('heading', { name: 'Offline music', exact: true })).toHaveCount(0);
    await page.getByRole('button', { name: 'More options', exact: true }).click();
    await page.locator('#settings-toggle').click();
    const settings = page.locator('#settings-modal');
    await expect(settings.getByRole('link', { name: 'Browse offline music' })).toHaveCount(0);
    await expect(settings.getByLabel('Playback cache budget')).toBeVisible();
    await expect(settings.getByRole('button', { name: 'Refresh saved collections', exact: true })).toBeVisible();
    await page.keyboard.press('Escape');
  }
  expect(await page.evaluate(() => localStorage.getItem('player-offline-v1-999:index'))).toBe('{"sentinel":true}');
  expect(await page.evaluate(async () => (await (await caches.open('player-offline-v1-999')).match('/saved-sentinel'))?.text())).toBe('saved bytes');
});

test('device files persist and standalone podcasts remain usable without sign-in', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/index.html?local=files');
  await page.locator('[data-file-input]').setInputFiles({
    name: 'Device song with a long readable title.wav', mimeType: 'audio/wav', buffer: Buffer.alloc(64),
  });
  await expect(page.locator('[data-results]')).toContainText('Device song with a long readable title.wav');
  await page.reload();
  await expect(page.locator('[data-results]')).toContainText('Device song with a long readable title.wav');
  await page.getByRole('link', { name: 'Podcasts', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Podcasts on this device', exact: true })).toBeVisible();
  await page.route('https://device-podcast.example/feed.xml', route => route.fulfill({
    contentType: 'application/rss+xml', body: '<rss version="2.0"><channel><title>Device podcast</title><item><guid>one</guid><title>Device episode</title><enclosure url="https://device-podcast.example/one.mp3" type="audio/mpeg"/></item></channel></rss>',
  }));
  page.once('dialog', dialog => dialog.accept('https://device-podcast.example/feed.xml'));
  await page.getByRole('button', { name: 'Add feed URL', exact: true }).click();
  await expect(page.locator('[data-results]')).toContainText('Device podcast');
  await page.reload();
  await expect(page.locator('[data-results]')).toContainText('Device podcast');
  await page.setViewportSize({ width: 844, height: 390 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
