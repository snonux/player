import { test, expect } from './helpers/scenario-fixture';
import { call, json } from './helpers/scenario-http';

// S01 performs first-run bootstrap itself through the browser.
test.use({ bootstrapAdmin: false });
test('S01: bootstrap form creates exactly one admin and browser login works', async ({ page, sandbox }) => {
  expect((await call('GET', '/healthz')).status).toBe(200);
  await page.goto('/');
  await expect(page).toHaveURL(/bootstrap.html/);
  await expect(page.locator('#bootstrap-form')).toBeVisible();
  await page.locator('#username').fill('admin');
  await page.locator('#password').fill('TestPassw0rd!');
  await page.locator('#password-confirm').fill('TestPassw0rd!');
  await page.getByRole('button', { name: 'Create Admin' }).click();
  await expect(page).toHaveURL(/login.html/);
  await page.locator('#username').fill('admin');
  await page.locator('#password').fill('TestPassw0rd!');
  await page.locator('button[type=submit]').click();
  await expect(page.locator('#admin-toggle')).toBeVisible();
  const cookie = (await page.context().cookies()).find(c => c.name === 'session');
  expect(cookie?.value).toBeTruthy();
  const users = await json<any[]>(await call('GET', '/api/v1/admin/users', { cookie: `session=${cookie!.value}` }));
  expect(users).toHaveLength(1);
  expect(users[0]).toMatchObject({ username: 'admin', is_admin: true });
  expect(sandbox.sql('SELECT count(*) FROM users WHERE is_admin=1')).toBe('1');
  expect((await call('POST', '/api/v1/auth/bootstrap', { body: { username: 'intruder', password: 'TestPassw0rd!' } })).status).toBe(403);
});
