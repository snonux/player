import { test, expect } from '@playwright/test';
import { bootstrap, triggerRescan, waitForServer } from './helpers/server';

test.use({ serviceWorkers: 'block' });
let adminCookie: string;

// Exercise the production router with real keyboard events. Editable children
// are added to the read-only dialogs to pin their ownership for future controls.
test.beforeAll(async () => {
  await waitForServer();
  adminCookie = await bootstrap();
  await triggerRescan(adminCookie);
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

test('shares preserves chords, typing and native buttons while row navigation works', async ({ page }) => {
  const card = page.locator('#media-grid .image-card').first();
  const id = Number(await card.getAttribute('data-id'));
  for (let i = 0; i < 2; i++) {
    expect((await page.request.post(`/api/media/${id}/shares`, { data: {} })).status()).toBe(200);
  }
  await page.keyboard.press('Shift+L');
  const modal = page.locator('#shares-modal');
  await expect(modal).toHaveClass(/open/);
  const selected = page.locator('#shares-list .share-row.selected');
  await expect(selected).toHaveAttribute('data-index', '0');
  for (const modifier of ['Control', 'Meta', 'Alt']) {
    await page.keyboard.press(`${modifier}+j`);
    await expect(selected).toHaveAttribute('data-index', '0');
  }
  await page.keyboard.press('j');
  await expect(selected).toHaveAttribute('data-index', '1');
  await page.keyboard.press('k');
  await expect(selected).toHaveAttribute('data-index', '0');

  await modal.evaluate(el => {
    const input = el.ownerDocument.createElement('input');
    input.id = 'guard-editable';
    el.querySelector('.modal')!.prepend(input);
  });
  const input = page.locator('#guard-editable');
  await input.focus();
  await page.keyboard.type('jkj');
  await expect(input).toHaveValue('jkj');
  await expect(selected).toHaveAttribute('data-index', '0');
  await expect(input).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(modal).not.toHaveClass(/open/);
  await expect(card).toBeFocused();

  await page.keyboard.press('Shift+L');
  await expect(modal).toHaveClass(/open/);
  const close = page.locator('#shares-close');
  await close.focus();
  await expect(close).toBeFocused();
  await page.keyboard.press('Space');
  await expect(modal).not.toHaveClass(/open/);
});

test('media info scrolls only for unmodified keys outside editable children', async ({ page }) => {
  await page.keyboard.press('i');
  await expect(page.locator('#media-info-modal')).toHaveClass(/open/);
  await expect(page.locator('#media-info-body table')).toBeVisible();
  const panel = page.locator('#media-info-panel');
  await panel.evaluate(el => {
    const input = el.ownerDocument.createElement('textarea');
    input.id = 'guard-editable';
    el.prepend(input);
    const spacer = el.ownerDocument.createElement('div');
    spacer.style.height = '3000px';
    el.append(spacer);
    el.scrollTop = 0;
  });
  await panel.focus();
  for (const modifier of ['Control', 'Meta', 'Alt']) {
    await page.keyboard.press(`${modifier}+j`);
    expect(await panel.evaluate(el => el.scrollTop)).toBe(0);
  }
  await page.keyboard.press('j');
  expect(await panel.evaluate(el => el.scrollTop)).toBeGreaterThan(0);
  await page.keyboard.press('k');
  expect(await panel.evaluate(el => el.scrollTop)).toBe(0);
  const input = page.locator('#guard-editable');
  await input.focus();
  await page.keyboard.type('jkj');
  await expect(input).toHaveValue('jkj');
  expect(await panel.evaluate(el => el.scrollTop)).toBe(0);
  await expect(input).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(page.locator('#media-info-modal')).not.toHaveClass(/open/);
  await expect(page.locator('#media-grid .image-card.selected')).toBeFocused();
});

test('modified Space leaves playback and sidebar selection alone', async ({ page }) => {
  // An unopened selected image gives Space a visible playback effect.
  const player = page.locator('#player');
  for (const modifier of ['Control', 'Meta', 'Alt']) {
    await page.keyboard.press(`${modifier}+Space`);
    await expect(player).not.toHaveClass(/open/);
  }
  await page.locator('#sidebar-toggle').focus();
  await page.keyboard.press('Enter');
  const row = page.locator('#sidebar .set-row').first();
  await row.focus();
  const item = row.locator('.set-item');
  const original = await item.getAttribute('class');
  for (const modifier of ['Control', 'Meta', 'Alt']) {
    await page.keyboard.press(`${modifier}+Space`);
    await expect(item).toHaveAttribute('class', original!);
    await expect(player).not.toHaveClass(/open/);
  }
  await page.keyboard.press('Space');
  await expect(item).not.toHaveAttribute('class', original!);
});
