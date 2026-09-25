import { test, expect, admin, browserLogin, createUser } from './helpers/scenario-fixture';
import { call } from './helpers/scenario-http';
test('S12: admin creates a usable account and deletion invalidates login', async () => {
  const { cookie, api } = await admin(); const before = await api('GET', '/admin/users');
  const user = await createUser(cookie);
  const added = await api('GET', '/admin/users'); expect(added).toHaveLength(before.length + 1);
  expect(added).toEqual(expect.arrayContaining([expect.objectContaining({ id: user.id, username: user.username })]));
  await api('DELETE', `/admin/users/${user.id}`);
  expect(await api('GET', '/admin/users')).toEqual(before);
  expect((await call('POST', '/api/v1/auth/login', { body: { username: user.username, password: 'TestPassw0rd!' } })).status).toBe(401);
});

test('S12: admin panel manages users, permissions and trash from the browser', async ({ page }) => {
  const { cookie, api, media, sets } = await admin();
  // Trash an image first so the grid can later show it coming back.
  const set = sets.find((s: any) => s.name === 'images');
  const item = media.find((m: any) => m.set_id === set.id);
  await api('DELETE', `/media/${item.id}`);
  await browserLogin(page, cookie);
  await page.goto('/');
  await page.getByRole('button', { name: 'Set images', exact: true }).click();
  const itemCard = page.locator(`#media-grid .media-card[data-id="${item.id}"]`);
  await expect(page.locator('#media-grid .image-card').first()).toBeVisible();
  await expect(itemCard).toHaveCount(0);
  // A opens the admin panel for admins.
  await page.keyboard.press('A');
  const panel = page.locator('#admin-modal');
  await expect(panel).toHaveClass(/open/);

  // Create a user through the form.
  const form = panel.locator('#admin-user-form');
  await form.getByPlaceholder('Username').fill('panel-user');
  await form.getByPlaceholder('Password').fill('TestPassw0rd!');
  await form.getByRole('button', { name: 'Add user' }).click();
  const remove = panel.getByRole('button', { name: 'Remove user panel-user' });
  await expect(remove).toBeVisible();
  const user = (await api('GET', '/admin/users')).find((u: any) => u.username === 'panel-user');

  // Grant a role with the labelled select; a failed change reverts.
  const select = panel.getByRole('combobox', { name: `Permission for panel-user on ${set.name}` });
  await select.selectOption('viewer');
  await expect.poll(async () => (await api('GET', '/admin/permissions')).permissions
    .some((p: any) => p.user_id === user.id && p.set_id === set.id && p.role === 'viewer')).toBe(true);
  await page.route(/\/api\/admin\/permissions$/, route => route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom"}' }));
  await select.selectOption('owner');
  await expect(page.locator('#toast')).toContainText('boom');
  await expect(select).toHaveValue('viewer');
  await expect(select).not.toHaveAttribute('aria-busy', 'true');
  await page.unrouteAll();

  // Quick successive changes are saved in order; the last one wins, even
  // when an earlier queued save fails.
  let failNext = true;
  await page.route(/\/api\/admin\/permissions$/, route => {
    if (failNext) {
      failNext = false;
      return route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"first failed"}' });
    }
    return route.fallback();
  });
  await select.selectOption('owner');
  await select.selectOption('');
  await expect(select).not.toHaveAttribute('aria-busy', 'true');
  await expect.poll(async () => (await api('GET', '/admin/permissions')).permissions
    .some((p: any) => p.user_id === user.id && p.set_id === set.id)).toBe(false);
  await expect(select).toHaveValue('');
  await page.unrouteAll();

  // Trash opens in its own section; the user list stays visible.
  const trashButton = panel.getByRole('button', { name: 'View trash' });
  await trashButton.click();
  await expect(panel.getByRole('button', { name: 'Hide trash' })).toHaveAttribute('aria-expanded', 'true');
  await expect(remove).toBeVisible();
  const restore = panel.getByRole('button', { name: `Restore ${item.file_name}` });
  await expect(restore).toBeVisible();
  // Deleted dates are formatted for people, not raw ISO timestamps.
  await expect(panel.locator('#admin-trash-list')).not.toContainText(/\d{4}-\d{2}-\d{2}T/);
  // Keyboard-restore: focus stays in the dialog after the list re-renders.
  await restore.focus();
  await page.keyboard.press('Enter');
  await expect(panel.locator('#admin-trash-list')).toContainText('No deleted items.');
  expect(await page.evaluate(() => Boolean((globalThis as any).document.activeElement?.closest('#admin-modal')))).toBe(true);
  // The grid behind the panel shows the restored item without a reload.
  await expect(itemCard).toHaveCount(1);
  expect((await api('GET', '/media?limit=1000')).some((m: any) => m.id === item.id)).toBe(true);

  // Removing a user asks first; dismissing keeps them.
  page.once('dialog', dialog => dialog.dismiss());
  await remove.click();
  await expect(remove).toBeVisible();
  expect((await api('GET', '/admin/users')).some((u: any) => u.id === user.id)).toBe(true);
  let confirmText = '';
  page.once('dialog', dialog => { confirmText = dialog.message(); dialog.accept(); });
  await remove.click();
  await expect(remove).toHaveCount(0);
  expect(confirmText).toContain('panel-user');
  expect((await api('GET', '/admin/users')).some((u: any) => u.id === user.id)).toBe(false);
});
