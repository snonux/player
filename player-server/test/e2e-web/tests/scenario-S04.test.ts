import { test, expect, admin } from './helpers/scenario-fixture';
import { call } from './helpers/scenario-http';
test('S04: public share renders media without granting application access and revokes', async ({ page }) => {
  const { api, media } = await admin();
  const item = media.find((m: any) => m.type === 'audio');
  const share = await api('POST', `/media/${item.id}/shares`);
  expect(await api('GET', `/media/${item.id}/shares`)).toEqual(expect.arrayContaining([expect.objectContaining({ token: share.token })]));
  expect((await page.goto(`/s/${share.token}`))!.status()).toBe(200);
  await expect(page.getByRole('button', { name: 'Play', exact: true })).toBeVisible();
  // Audio is shown by its display title: the tag title, else the name without extension.
  await expect(page.locator('h1')).toHaveText(item.music?.title?.trim() || item.file_name.replace(/\.[^.]+$/, ''));
  await page.goto('/');
  await expect(page).toHaveURL(/login.html/);
  expect((await call('GET', '/api/v1/media')).status).toBe(401);
  await api('DELETE', `/shares/${share.token}`);
  expect((await page.goto(`/s/${share.token}`))!.status()).toBe(404);
});
