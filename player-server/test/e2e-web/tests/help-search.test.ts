import { test, expect } from '@playwright/test';
import { bootstrap, triggerRescan, waitForServer } from './helpers/server';

test.use({ serviceWorkers: 'block' });
let adminCookie: string;
test.beforeAll(async () => {
  await waitForServer();
  adminCookie = await bootstrap();
  await triggerRescan(adminCookie);
  const baseURL = process.env.PLAYER_URL || 'http://localhost:8080';
  await expect.poll(async () => {
    const res = await fetch(`${baseURL}/api/media?type=image`, { headers: { Cookie: adminCookie } });
    return ((await res.json()) || []).length;
  }, { timeout: 30_000 }).toBeGreaterThanOrEqual(2);
});
test.beforeEach(async ({ page }) => {
  const origin = new URL(process.env.PLAYER_URL || 'http://localhost:8080');
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.goto('/');
  await page.getByRole('button', { name: 'Set images', exact: true }).click();
  const card = page.locator('#media-grid .image-card').first();
  await expect(card).toBeVisible();
  await card.focus();
});

test('help filters rendered rows and keeps native typing, focus and library search', async ({ page }) => {
  const help = page.locator('#help-modal');
  const input = page.getByRole('searchbox', { name: 'Filter keyboard shortcuts' });
  const rows = help.locator('.help-row');
  const count = await rows.count();
  await page.keyboard.press('?');
  await expect(help).toBeVisible();
  await expect(input).toBeFocused();
  await expect(help.locator('.help-row:visible')).toHaveCount(count);
  await input.fill('FuLlScReEn');
  const matching = rows.filter({ hasText: /fullscreen/i });
  await expect(help.locator('.help-row:visible')).toHaveCount(await matching.count());
  await expect(rows.filter({ hasText: 'Upload to current set' })).toBeHidden();
  await input.fill('Shift+↑');
  await expect(help.locator('.help-row:visible')).toHaveCount(1);
  await expect(rows.filter({ hasText: 'Nudge display crop position' })).toBeVisible();
  await input.fill('nonexistent shortcut');
  await expect(help.locator('.help-row:visible')).toHaveCount(0);
  await expect(page.locator('#help-no-results')).toBeVisible();
  await input.fill('');
  await expect(help.locator('.help-row:visible')).toHaveCount(count);
  await expect(page.locator('#help-no-results')).toBeHidden();
  await page.keyboard.type('/?jkc');
  await expect(input).toHaveValue('/?jkc');
  await page.keyboard.press('Enter');
  await expect(input).toBeFocused();
  await expect(help).toHaveClass(/open/);
  const close = page.locator('#help-close');
  await close.focus();
  for (const modifier of ['Control', 'Meta', 'Alt']) {
    await page.keyboard.press(`${modifier}+/`);
    await expect(close).toBeFocused();
  }
  await page.keyboard.press('/');
  await expect(input).toBeFocused();
  await page.keyboard.type('zoom'); // slash selects the existing query
  await expect(input).toHaveValue('zoom');
  await page.keyboard.press('Tab');
  await expect(close).toBeFocused();
  await page.keyboard.press('Space');
  await expect(help).not.toHaveClass(/open/);
  await expect(page.locator('#media-grid .image-card.selected')).toBeFocused();
  await page.keyboard.press('/');
  await expect(page.locator('#search-input')).toBeFocused();
  await page.keyboard.type('?');
  await expect(help).not.toHaveClass(/open/);
  await page.keyboard.press('Escape');
  await page.keyboard.press('n');
  await expect(page.locator('#notes-modal')).toHaveClass(/open/);
  await page.locator('#notes-close').focus();
  await page.keyboard.press('/');
  await page.keyboard.press('?');
  await expect(help).not.toHaveClass(/open/);
});

test('expanded and fullscreen image help preserves image state and restores its opener', async ({ page }) => {
  const help = page.locator('#help-modal');
  const player = page.locator('#player');
  const image = page.locator('#media-image');
  await page.keyboard.press('Enter');
  await expect(player).toHaveClass(/open.*has-image/);
  const zoom = page.locator('#btn-zoom-in');
  await zoom.click();
  await expect(image).toHaveCSS('transform', 'matrix(1.25, 0, 0, 1.25, 0, 0)');
  const src = await image.getAttribute('src');
  const transform = await image.evaluate(el => getComputedStyle(el).transform);
  for (const fullscreen of [false, true]) {
    if (fullscreen) {
      await page.locator('#btn-fullscreen').click();
      await expect.poll(() => page.evaluate(() => document.fullscreenElement?.id)).toBe('player');
      await page.evaluate(async () => {
        const path = '/js/playback.js';
        const { toggleCrop } = await import(path);
        toggleCrop();
      });
      await expect(player).toHaveClass(/crop-mode/);
    }
    await zoom.focus();
    await page.keyboard.press('?');
    await expect(help).toBeVisible();
    await expect(page.locator('#help-search')).toBeFocused();
    await page.locator('#help-close').focus();
    await page.keyboard.press('/');
    await expect(page.locator('#help-search')).toBeFocused();
    await page.keyboard.type('zoom');
    await expect(help.locator('.help-row:visible')).toHaveCount(await help.locator('.help-row').filter({ hasText: /zoom/i }).count());
    await page.keyboard.press('Escape');
    await expect(help).not.toHaveClass(/open/);
    await expect(zoom).toBeFocused();
    await expect(player).toHaveClass(/open.*has-image/);
    await expect(image).toHaveAttribute('src', src!);
    if (fullscreen) {
      await expect.poll(() => page.evaluate(() => document.fullscreenElement?.id)).toBe('player');
      await expect(player).toHaveClass(/crop-mode/);
    } else {
      await expect(image).toHaveCSS('transform', transform);
    }
    await expect.poll(() => help.evaluate(el => el.parentElement?.tagName)).toBe('BODY');
  }
  await zoom.focus();
  await page.keyboard.press('?');
  await expect(help).toBeVisible();
  await page.evaluate(() => document.exitFullscreen());
  await expect.poll(() => page.evaluate(() => document.fullscreenElement)).toBeNull();
  await expect(help).toBeVisible();
  await expect(page.locator('#help-search')).toBeFocused();
  await expect.poll(() => help.evaluate(el => el.parentElement?.tagName)).toBe('BODY');
  await page.keyboard.press('Escape');
  await expect(help).not.toHaveClass(/open/);
  await expect(zoom).toBeFocused();
});
