import { test, expect, type Page } from '@playwright/test';
import { bootstrap, triggerRescan, waitForServer } from './helpers/server';

let adminCookie: string;

test.beforeAll(async () => {
  test.setTimeout(100_000);
  await waitForServer();
  adminCookie = await bootstrap();
  await triggerRescan(adminCookie);
});

async function openApp(page: Page) {
  const sessionCookie = await bootstrap();
  const origin = new URL(process.env.PLAYER_URL || 'http://localhost:8080');
  await page.context().addCookies([{
    name: 'session', value: sessionCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.goto('/');
  await expect(page.locator('#admin-toggle')).not.toHaveClass(/hidden/);
}

test.describe('touch header', () => {
  test.use({
    viewport: { width: 393, height: 851 }, deviceScaleFactor: 2.75,
    isMobile: true, hasTouch: true, serviceWorkers: 'block',
  });

  test('theme, Admin settings, sets, and logout are reachable by tapping', async ({ page }) => {
    await openApp(page);
    for (const width of [393, 320]) {
      await page.setViewportSize({ width, height: 851 });
      const viewport = page.viewportSize();
      expect(viewport).not.toBeNull();
      for (const id of ['theme-toggle', 'admin-toggle', 'sidebar-toggle', 'logout-btn']) {
        const control = page.locator(`#${id}`);
        await expect(control).toBeVisible();
        const box = await control.boundingBox();
        expect(box).not.toBeNull();
        expect(box!.x).toBeGreaterThanOrEqual(0);
        expect(box!.y).toBeGreaterThanOrEqual(0);
        expect(box!.x + box!.width).toBeLessThanOrEqual(viewport!.width);
        expect(box!.y + box!.height).toBeLessThanOrEqual(viewport!.height);
      }
    }
    const initialTheme = await page.locator('html').getAttribute('data-theme');
    await page.locator('#theme-toggle').tap();
    await expect(page.locator('html')).not.toHaveAttribute('data-theme', initialTheme || 'dark');

    await page.locator('#admin-toggle').tap();
    await expect(page.locator('#admin-modal')).toHaveClass(/open/);
    await page.locator('#admin-close').tap();
    await expect(page.locator('#admin-modal')).not.toHaveClass(/open/);

    await page.locator('#sidebar-toggle').tap();
    await expect(page.locator('#sidebar')).toHaveClass(/open/);
    const setRow = page.locator('#set-list .set-row').first();
    await expect(setRow).toBeVisible();
    await setRow.tap();
    await expect(setRow.locator('.set-item')).toHaveClass(/active/);
    await page.locator('#menu-close').tap();
    await expect(page.locator('#sidebar')).not.toHaveClass(/open/);

    await page.locator('#logout-btn').tap();
    await expect(page).toHaveURL(/\/login\.html$/);
  });
});

test.describe('desktop header', () => {
  test.use({ viewport: { width: 1280, height: 800 }, serviceWorkers: 'block' });

  test('hover still reveals header controls', async ({ page }) => {
    await openApp(page);
    await page.mouse.move(640, 400);
    await page.mouse.move(640, 2);
    await expect(page.locator('#theme-toggle')).toBeInViewport();
    await page.locator('#theme-toggle').click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  });
});
