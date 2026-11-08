import { test, expect, type Page } from '@playwright/test';
import { bootstrap, triggerRescan, waitForScanIdle, waitForServer } from './helpers/server';

// UX revamp: the Browse sidebar, the visible Search button and the grouped
// More options menu, used by keyboard (vi keys) and by pointer.

test.use({ serviceWorkers: 'block' });

const baseURL = process.env.PLAYER_URL || 'http://localhost:8080';
let adminCookie: string;

test.beforeAll(async () => {
  test.setTimeout(100_000);
  await waitForServer();
  adminCookie = await bootstrap();
  await triggerRescan(adminCookie);
  await waitForScanIdle(adminCookie);
});

test.beforeEach(async ({ page }) => {
  const origin = new URL(baseURL);
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.goto('/');
  await expect(page.locator('#media-grid .set-card').first()).toBeVisible();
});

const sidebar = (page: Page) => page.locator('#sidebar');

test('b opens Browse with focus inside; j/k move, Esc closes and returns focus', async ({ page }) => {
  await page.keyboard.press('b');
  await expect(sidebar(page)).toHaveClass(/open/);
  await expect(page.locator('#sidebar-toggle')).toHaveAttribute('aria-expanded', 'true');
  for (const heading of ['Music', 'Podcasts', 'Radio', 'Sets']) {
    await expect(sidebar(page).getByRole('heading', { name: heading, exact: true })).toBeVisible();
  }
  const focused = () => page.evaluate(() => document.activeElement?.id || document.activeElement?.getAttribute('aria-label'));
  expect(await page.evaluate(() => Boolean(document.activeElement?.closest('#sidebar')))).toBe(true);
  const first = await focused();
  await page.keyboard.press('j');
  expect(await focused()).not.toBe(first);
  await page.keyboard.press('k');
  expect(await focused()).toBe(first);
  // Sets show the number key that opens them.
  await expect(page.locator('#set-list .set-row').first().locator('kbd')).toHaveText('1');
  await page.keyboard.press('Escape');
  await expect(sidebar(page)).not.toHaveClass(/open/);
  expect(await page.evaluate(() => Boolean(document.activeElement?.closest('#sidebar')))).toBe(false);
  // b toggles from inside the sidebar too.
  await page.keyboard.press('b');
  await expect(sidebar(page)).toHaveClass(/open/);
  await page.keyboard.press('b');
  await expect(sidebar(page)).not.toHaveClass(/open/);
});

test('global vi keys keep working while the sidebar stays open', async ({ page }) => {
  await page.locator('#sidebar-toggle').click();
  await expect(sidebar(page)).toHaveClass(/open/);
  await page.locator('#media-grid .set-card').first().focus();
  await page.keyboard.press('?');
  await expect(page.locator('#help-modal')).toHaveClass(/open/);
  await page.keyboard.press('Escape');
  await expect(page.locator('#help-modal')).not.toHaveClass(/open/);
  await expect(sidebar(page)).toHaveClass(/open/);
});

test('Search button and Radio section offer pointer paths to keyboard features', async ({ page }) => {
  await page.getByRole('button', { name: 'Search library', exact: true }).click();
  await expect(page.locator('#search-input')).toBeFocused();
  await page.keyboard.press('Escape');
  await page.locator('#sidebar-toggle').click();
  await expect(sidebar(page).getByRole('button', { name: 'Internet radio', exact: true })).toBeVisible();
  await expect(sidebar(page).getByRole('button', { name: 'Web radio', exact: true })).toBeVisible();
});

test('More options groups its items and shows their global keys', async ({ page }) => {
  await page.locator('#header-menu-toggle').click();
  const menu = page.locator('#header-menu');
  await expect(menu).toBeVisible();
  await expect(menu.locator('.menu-label')).toHaveText(['Browse', 'View', 'Help & account']);
  await expect(page.locator('#radio-toggle .menu-kbd')).toHaveText('w');
  await expect(page.locator('#shuffle-toggle .menu-kbd')).toHaveText('r');
  // Inside the menu the same letter picks the item.
  await expect(page.locator('#radio-toggle')).toHaveAttribute('data-shortcut-key', 'w');
});

test('help groups shortcuts and hides empty groups while filtering', async ({ page }) => {
  await page.keyboard.press('?');
  const help = page.locator('#help-modal');
  await expect(help.locator('.help-group').first()).toBeVisible();
  await expect(help.locator('.help-row', { hasText: 'Browse sidebar' })).toBeVisible();
  await page.locator('#help-search').fill('Browse sidebar');
  await expect(help.locator('.help-section:visible')).toHaveCount(1);
  await expect(help.locator('.help-row:visible')).toHaveCount(1);
});
