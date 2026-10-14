import { randomBytes } from 'node:crypto';
import { test, expect } from '@playwright/test';
import { triggerRescan } from './helpers/server';

// These tests mutate media. Run only on the isolated instance documented in
// k8s/test-trash, with its own DB and library. No production credentials.
let adminCookie: string;
let setId: number;
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a3ioAAAAASUVORK5CYII=', 'base64');

test.beforeAll(async ({ request }) => {
  expect(process.env.PLAYER_TRASH_TEST_INSTANCE, 'explicit isolated-instance opt-in').toBe('1');
  const username = process.env.E2E_ADMIN_USER;
  const password = process.env.E2E_ADMIN_PASS;
  expect(username, 'isolated admin username required').toBeTruthy();
  expect(password, 'isolated admin password required').toBeTruthy();
  let auth = await request.post('/api/bootstrap', { data: { username, password } });
  if (auth.status() === 403) auth = await request.post('/api/login', { data: { username, password } });
  expect(auth.status()).toBe(200);
  if (new URL(process.env.PLAYER_URL!).protocol === 'https:') expect(auth.headers()['set-cookie']).toContain('Secure');
  const session = auth.headers()['set-cookie']?.match(/session=([^;]+)/)?.[1];
  expect(session).toBeTruthy();
  adminCookie = `session=${session}`;
  await triggerRescan(adminCookie);
  const response = await request.get('/api/sets', { headers: { Cookie: adminCookie } });
  const sets = await response.json();
  expect(sets.length).toBeGreaterThan(0);
  setId = sets.find((set: { name: string }) => set.name === 'trash-tests')?.id || sets[0].id;
});

test.beforeEach(async ({ page }) => {
  const origin = new URL(process.env.PLAYER_URL || 'http://127.0.0.1:18083');
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Lax', secure: origin.protocol === 'https:', httpOnly: true,
  }]);
});

async function upload(request: any, name: string) {
  const response = await request.post(`/api/sets/${setId}/upload`, {
    headers: { Cookie: adminCookie },
    multipart: { file: { name, mimeType: 'image/png', buffer: png } },
  });
  expect(response.status()).toBe(200);
  return await response.json();
}

test('card delete, cancel, trash timestamp, keyboard restore, grid refresh and Escape', async ({ page }) => {
  const item = await upload(page.request, `trash-browser-${Date.now()}.png`);
  await page.goto('/');
  await page.locator(`.set-open[data-set="${setId}"], .set-card[data-set-id="${setId}"] .set-open`).first().click();
  const card = page.locator(`.media-card[data-id="${item.id}"]`);
  await expect(card).toBeVisible();
  page.once('dialog', dialog => dialog.dismiss());
  await card.getByRole('button', { name: 'Move to trash', exact: true }).click();
  await expect(card).toBeVisible();
  page.once('dialog', dialog => dialog.accept());
  await card.getByRole('button', { name: 'Move to trash', exact: true }).click();
  await expect(card).toHaveCount(0);
  expect((await page.request.get(`/api/media/${item.id}`)).status()).toBe(404);
  const trash = await (await page.request.get('/api/trash')).json();
  expect(trash.find((m: any) => m.id === item.id).deleted_at).toBeTruthy();
  const toggle = page.getByRole('button', { name: 'Open trash', exact: true });
  await toggle.focus();
  await toggle.click();
  const modal = page.locator('#trash-modal');
  await expect(modal).toHaveClass(/open/);
  const restore = modal.getByRole('button', { name: `Restore ${item.file_name}`, exact: true });
  await expect(restore).toBeVisible();
  await expect(modal).toContainText('Deleted');
  await expect(modal).toContainText('trash-tests');
  await expect(modal).toContainText('Expires');
  await page.screenshot({ path: '/tmp/player-oc3-trash.png' });
  await restore.focus();
  await page.keyboard.press('Enter');
  await expect(restore).toHaveCount(0);
  await expect(card).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(modal).not.toHaveClass(/open/);
  await expect(page.locator('.modal-overlay.open')).toHaveCount(0);
});

test('trash j/k/arrows and Tab keep focus inside; native Space closes', async ({ page }) => {
  for (let i = 0; i < 2; i++) {
    const item = await upload(page.request, `trash-navigation-${Date.now()}-${i}.png`);
    expect((await page.request.delete(`/api/media/${item.id}`, { headers: { Cookie: adminCookie } })).status()).toBe(200);
  }
  await page.goto('/');
  await page.getByRole('button', { name: 'Open trash', exact: true }).focus();
  await page.getByRole('button', { name: 'Open trash', exact: true }).click();
  const buttons = page.locator('#trash-list button[data-id]');
  await expect(buttons.nth(1)).toBeVisible();
  await buttons.first().focus();
  await page.keyboard.press('Control+j');
  await expect(buttons.first()).toBeFocused();
  await page.keyboard.press('j');
  await expect(buttons.nth(1)).toBeFocused();
  await page.keyboard.press('k');
  await expect(buttons.first()).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(buttons.nth(1)).toBeFocused();
  await buttons.last().focus();
  await page.keyboard.press('Tab');
  await expect(page.locator('#trash-close')).toBeFocused();
  await page.keyboard.press('Space');
  await expect(page.locator('#trash-modal')).not.toHaveClass(/open/);
});

test('owner sees/restores only owned trash; viewer and outsider cannot restore or delete', async ({ page }) => {
  const name = `trash-owner-${randomBytes(8).toString("hex")}`;
  const password = randomBytes(24).toString("base64url");
  const userRes = await page.request.post('/api/admin/users', {
    headers: { Cookie: adminCookie }, data: { username: name, password, is_admin: false },
  });
  expect(userRes.status()).toBe(200);
  const user = await userRes.json();
  const item = await upload(page.request, `trash-owner-${Date.now()}.png`);
  try {
    expect((await page.request.post('/api/admin/permissions', {
      headers: { Cookie: adminCookie }, data: { set_id: setId, user_id: user.id, role: 'viewer' },
    })).status()).toBe(200);
    await page.context().clearCookies();
    expect((await page.request.post('/api/login', { data: { username: name, password } })).status()).toBe(200);
    expect((await page.request.delete(`/api/media/${item.id}`)).status()).toBe(403);
    expect((await page.request.post(`/api/media/${item.id}/restore`)).status()).toBe(403);
    expect(await (await page.request.get('/api/trash')).json()).toEqual([]);
    expect((await page.request.post('/api/admin/permissions', {
      headers: { Cookie: adminCookie }, data: { set_id: setId, user_id: user.id, role: 'owner' },
    })).status()).toBe(200);
    expect((await page.request.delete(`/api/media/${item.id}`)).status()).toBe(200);
    await page.goto('/');
    await page.getByRole('button', { name: 'Open trash', exact: true }).focus();
  await page.getByRole('button', { name: 'Open trash', exact: true }).click();
    await page.getByRole('button', { name: `Restore ${item.file_name}`, exact: true }).press('Enter');
    expect((await page.request.get(`/api/media/${item.id}`)).status()).toBe(200);
    await page.request.delete('/api/admin/permissions', {
      headers: { Cookie: adminCookie }, data: { set_id: setId, user_id: user.id },
    });
    expect((await page.request.delete(`/api/media/${item.id}`)).status()).toBe(403);
    expect((await page.request.post(`/api/media/${item.id}/restore`)).status()).toBe(403);
  } finally {
    await page.request.delete(`/api/admin/users/${user.id}`, { headers: { Cookie: adminCookie } });
  }
});
