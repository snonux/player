import { test, expect } from '@playwright/test';
import { bootstrap, triggerRescan, waitForServer } from './helpers/server';

test.use({ serviceWorkers: 'block' });

const baseURL = process.env.PLAYER_URL || 'http://localhost:8080';
let adminCookie: string;

test.beforeAll(async () => {
  test.setTimeout(100_000);
  await waitForServer();
  adminCookie = await bootstrap();
  await triggerRescan(adminCookie);
});

test.beforeEach(async ({ page }) => {
  const origin = new URL(baseURL);
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.goto('/');
});

test('set center click and keyboard open the set; explicit cover button regenerates', async ({ page }) => {
  let coverPosts = 0;
  page.on('request', request => {
    if (request.method() === 'POST' && /\/api\/sets\/\d+\/cover$/.test(request.url())) coverPosts++;
  });
  await page.getByRole('button', { name: 'Set images' }).click();
  await expect(page.locator('#media-grid .image-card').first()).toBeVisible();
  expect(coverPosts).toBe(0);

  await page.keyboard.press('Backspace');
  await expect(page.getByRole('button', { name: 'Set images' })).toBeVisible();
  await page.getByRole('button', { name: 'Set audiobooks' }).focus();
  await page.keyboard.press('ArrowRight');
  await expect(page.locator('.set-card').filter({ hasText: 'images' })).toHaveClass(/selected/);
  await page.keyboard.press('Enter');
  await expect(page.locator('#media-grid .image-card').first()).toBeVisible();
  expect(coverPosts).toBe(0);

  await page.keyboard.press('Backspace');
  const regenerate = page.getByRole('button', { name: 'Regenerate cover for images' });
  const request = page.waitForRequest(req => req.method() === 'POST' && /\/api\/sets\/\d+\/cover$/.test(req.url()));
  await regenerate.click();
  await request;
  await expect(page.locator('#toast')).toContainText('Set cover regenerated');
  await expect(page.getByRole('button', { name: 'Set images' })).toBeVisible();
});

test('Space on a focused set button plays/pauses instead of opening a set', async ({ page }) => {
  const audiobooks = page.getByRole('button', { name: 'Set audiobooks' });
  await audiobooks.focus();
  await page.keyboard.press('ArrowRight');
  await expect(page.locator('.set-card').filter({ hasText: 'images' })).toHaveClass(/selected/);
  await page.keyboard.press(' ');
  // Focus stayed on the audiobooks button; a native click would open it.
  await expect(audiobooks).toBeVisible();
  await expect(page.locator('#media-grid .media-card:not(.set-card)')).toHaveCount(0);
  await expect(page.locator('#breadcrumb-bar')).not.toContainText('audiobooks');
});

test.describe('touch set card', () => {
  test.use({ hasTouch: true, viewport: { width: 320, height: 640 } });

  test('center tap opens without regenerating cover', async ({ page }) => {
    let coverPosts = 0;
    page.on('request', request => {
      if (request.method() === 'POST' && /\/api\/sets\/\d+\/cover$/.test(request.url())) coverPosts++;
    });
    await page.getByRole('button', { name: 'Set images' }).tap();
    await expect(page.locator('#media-grid .image-card').first()).toBeVisible();
    expect(coverPosts).toBe(0);
  });
});
