import { copyFileSync, mkdirSync } from 'node:fs';
import path from 'node:path';
import { test, expect, admin, browserLogin, scan } from './helpers/scenario-fixture';
import { call } from './helpers/scenario-http';
test('S25: SQL, literal wildcard, XSS storage and public traversal probes', async () => {
  const { cookie, api, media } = await admin(); const users = await api('GET', '/admin/users'); const id = media[0].id;
  for (const [key, value] of [['search', "' OR 1=1 --"], ['search', "'; DROP TABLE users; --"], ['search', '%'], ['tags', "' OR ''='"]]) expect(await api('GET', `/media?${new URLSearchParams({ [key]: value })}`)).toEqual([]);
  expect(await api('GET', `/media?${new URLSearchParams({ set_id: '1 OR 1=1' })}`)).toEqual(media);
  const union = await api('GET', `/media?${new URLSearchParams({ set_ids: '1,2,3) UNION SELECT * FROM users --' })}`);
  expect(Array.isArray(union)).toBe(true); for (const item of union) { expect(item).toMatchObject({ id: expect.any(Number), set_id: expect.any(Number), rel_path: expect.any(String) }); expect(item).not.toHaveProperty('password_hash'); }
  expect(await api('GET', `/media?${new URLSearchParams({ sort: 'name; DROP TABLE media; --' })}`)).toEqual(media);
  const content = '<script>alert(1)</script>'; expect(await api('POST', `/media/${id}/notes`, { content })).toMatchObject({ content }); expect(await api('GET', `/media/${id}/notes`)).toMatchObject({ content });
  const tag = '<img src=x onerror=alert(1)>'; await api('POST', `/media/${id}/tags`, { tag }); expect(await api('GET', '/tags')).toEqual(expect.arrayContaining([expect.objectContaining({ name: tag })]));
  const reflection = await call('GET', `/api/v1/media?${new URLSearchParams({ search: '<script>alert(2)</script>', limit: 'foo' })}`, { cookie }); expect(reflection.status).toBe(200); expect(await reflection.text()).not.toContain('<script>alert(2)</script>');
  await api('DELETE', `/media/${id}/notes`); await api('DELETE', `/media/${id}/tags/${encodeURIComponent(tag)}`);
  // URL parsing folds /s/../ away, so this reaches the session-guarded admin route.
  expect((await call('GET', '/s/../admin/users')).status).toBe(401);
  for (const route of ['/s/..%2Fadmin%2Fusers', '/s/%2E%2E%2Fadmin%2Fusers/stream']) expect((await call('GET', route)).status).toBe(404);
  expect(await api('GET', '/admin/users')).toEqual(users); expect(await api('GET', '/media?limit=1000')).toEqual(media);
});

test('S25: hostile file and folder names render as inert text in the browser', async ({ page, sandbox }) => {
  const folder = `x"<img src=x onerror=window.__xss=1>`;
  const file = `a&b "c" <img src=x onerror=window.__xss=2>.jpg`;
  // Image sets list files flat; audiobooks shows folder cards.
  copyFileSync(path.join(sandbox.mediaRoot, 'images/earth-pia18033.jpg'), path.join(sandbox.mediaRoot, 'images', file));
  mkdirSync(path.join(sandbox.mediaRoot, 'audiobooks', folder));
  // Single-file folders are flattened into their parent, so use two files.
  for (const name of ['01.mp3', '02.mp3']) copyFileSync(path.join(sandbox.mediaRoot, 'audiobooks/aesops-fables/37-chapter.mp3'), path.join(sandbox.mediaRoot, 'audiobooks', folder, name));
  const { cookie, api } = await admin();
  await scan(cookie);
  const item = (await api('GET', `/media?${new URLSearchParams({ search: 'a&b', limit: '1000' })}`)).find((m: any) => m.file_name === file);
  expect(item).toBeTruthy();

  await browserLogin(page, cookie);
  await page.goto('/');
  await page.getByRole('button', { name: 'Set images' }).click();
  await expect(page.locator(`#media-grid .image-card[data-id="${item.id}"]`)).toHaveAttribute('aria-label', file);
  await expect(page.locator(`#media-grid .image-card[data-id="${item.id}"] .title`)).toHaveText(file);
  expect(await page.locator('#media-grid img[src="x"]').count()).toBe(0);
  await page.keyboard.press('Backspace');
  await page.getByRole('button', { name: 'Set audiobooks' }).click();
  await expect(page.getByRole('button', { name: `Folder ${folder}`, exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: `Regenerate cover for folder ${folder}`, exact: true })).toBeVisible();
  expect(await page.locator('#media-grid img[src="x"]').count()).toBe(0);

  const share = await api('POST', `/media/${item.id}/shares`);
  await page.goto(`/s/${share.token}`);
  await expect(page.locator('h1')).toHaveText(file);
  await expect(page).toHaveTitle(`${file} — Shared Media`);
  expect(await page.locator('h1 img, img[src="x"]').count()).toBe(0);
  expect(await page.evaluate(() => (globalThis as any).__xss)).toBeUndefined();
});
