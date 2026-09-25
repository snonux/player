import { test, expect, type Page } from '@playwright/test';
import { bootstrap, triggerRescan, waitForServer } from './helpers/server';

const baseURL = process.env.PLAYER_URL || 'http://localhost:8080';
let adminCookie: string;
let setId: number;

test.beforeAll(async () => {
  test.setTimeout(100_000);
  await waitForServer();
  adminCookie = await bootstrap();
  await triggerRescan(adminCookie);
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    const sets = await (await fetch(`${baseURL}/api/sets`, { headers: { Cookie: adminCookie } })).json() as Array<{ id: number }>;
    for (const set of sets) {
      const browse = await (await fetch(`${baseURL}/api/sets/${set.id}/browse`, { headers: { Cookie: adminCookie } })).json() as { media?: Array<unknown> };
      if (browse.media?.length) {
        setId = set.id;
        return;
      }
    }
    await new Promise(resolve => setTimeout(resolve, 500));
  }
  throw new Error('Need a set with media at its root for share clipboard tests');
});

async function openApp(page: Page, clipboard: 'absent' | 'denied' | 'working') {
  await page.addInitScript((mode) => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: mode === 'absent' ? undefined : {
        writeText: mode === 'denied'
          ? () => Promise.reject(new Error('Write permission denied'))
          : () => Promise.resolve(),
      },
    });
  }, clipboard);
  const origin = new URL(baseURL);
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.goto('/');
  await page.locator(`.set-row[data-id="${setId}"]`).waitFor({ state: 'attached' });
  await page.evaluate(async (id) => {
    const modulePath = '/js/views/sets.js';
    const { selectSet } = await import(modulePath);
    selectSet(id);
  }, setId);
  const card = page.locator('#media-grid .media-card').first();
  await expect(card).toBeVisible();
  return card;
}

async function expectManualLink(page: Page) {
  const modal = page.locator('#share-link-fallback-modal');
  await expect(modal).toHaveClass(/open/);
  const input = page.locator('#share-link-fallback-url');
  await expect(input).toHaveValue(/^https?:\/\/[^/]+\/s\/[A-Za-z0-9_-]+$/);
  const selected = await input.evaluate((el: any) =>
    el.selectionStart === 0 && el.selectionEnd === el.value.length);
  expect(selected).toBe(true);
  await expect(page.locator('#toast')).toContainText('Clipboard unavailable');
  await expect(page.locator('#toast')).not.toContainText('copied');
}

test('s shortcut exposes a selectable link when clipboard API is absent', async ({ page }) => {
  const card = await openApp(page, 'absent');
  await card.click();
  await page.keyboard.press('s');
  await expectManualLink(page);
  await page.locator('#share-link-fallback-close').click();
  await expect(page.locator('#share-link-fallback-modal')).not.toHaveClass(/open/);
  await expect(card).toBeFocused();
  await page.keyboard.press('s');
  await expectManualLink(page);
  await page.locator('#share-link-fallback-modal').click({ position: { x: 5, y: 5 } });
  await expect(page.locator('#share-link-fallback-modal')).not.toHaveClass(/open/);
  await expect(card).toBeFocused();
  await page.keyboard.press('s');
  await expectManualLink(page);
});

test('My Shares copy button recovers from rejected clipboard writes', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  const card = await openApp(page, 'denied');
  const id = Number(await card.getAttribute('data-id'));
  const created = await page.request.post(`/api/media/${id}/shares`, { data: {} });
  expect(created.status()).toBe(200);
  await page.keyboard.press('Shift+L');
  await expect(page.locator('#shares-modal')).toHaveClass(/open/);
  await page.locator('#shares-list [data-copy]').first().click();
  await expectManualLink(page);
  await page.locator('#share-link-fallback-close').click();
  await expect(page.locator('#shares-modal')).toHaveClass(/open/);
  await expect(page.locator('#shares-list [data-copy]').first()).toBeFocused();
  expect(errors).toEqual([]);
});

test('My Shares Enter shortcut recovers from rejected clipboard writes', async ({ page }) => {
  const card = await openApp(page, 'denied');
  const id = Number(await card.getAttribute('data-id'));
  const created = await page.request.post(`/api/media/${id}/shares`, { data: {} });
  expect(created.status()).toBe(200);
  await page.keyboard.press('Shift+L');
  await expect(page.locator('#shares-modal')).toHaveClass(/open/);
  await page.keyboard.press('Enter');
  await expectManualLink(page);
  await page.keyboard.press('Escape');
  await expect(page.locator('#share-link-fallback-modal')).not.toHaveClass(/open/);
  await expect(page.locator('#shares-modal')).toHaveClass(/open/);
  await expect(page.locator('#shares-list .share-row.selected')).toBeFocused();
});

test('successful clipboard write reports success without opening fallback', async ({ page }) => {
  const card = await openApp(page, 'working');
  await card.click();
  await page.keyboard.press('s');
  await expect(page.locator('#toast')).toContainText('Share link copied');
  await expect(page.locator('#share-link-fallback-modal')).not.toHaveClass(/open/);
});

test('Enter activates focused Revoke and Close buttons in My Shares', async ({ page }) => {
  const card = await openApp(page, 'working');
  const id = Number(await card.getAttribute('data-id'));
  const created = await page.request.post(`/api/media/${id}/shares`, { data: {} });
  expect(created.status()).toBe(200);
  const { token } = await created.json() as { token: string };

  await page.keyboard.press('Shift+L');
  await expect(page.locator('#shares-modal')).toBeVisible();
  const revoke = page.locator(`#shares-list [data-revoke="${token}"]`);
  await revoke.focus();
  await expect(revoke).toBeFocused();
  const revoked = page.waitForResponse(response =>
    response.url().endsWith(`/api/shares/${token}`) && response.request().method() === 'DELETE');
  await page.keyboard.press('Enter');
  expect((await revoked).status()).toBe(200);
  await expect(revoke).toHaveCount(0);
  await expect(revoke).toHaveCount(0);
  await expect(page.locator('#toast')).toContainText('Share revoked');

  await page.locator('#shares-close').focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('#shares-modal')).not.toHaveClass(/open/);
  await expect(page.locator('#toast')).not.toContainText('copied');
});
