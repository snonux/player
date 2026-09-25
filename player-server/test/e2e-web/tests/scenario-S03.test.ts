import { test, expect, admin, upload, scan, browserLogin } from './helpers/scenario-fixture';
import { json, call } from './helpers/scenario-http';
test('S03: Bearer upload survives rescan and appears in browser search', async ({ page, sandbox }) => {
  const { cookie, api, sets } = await admin();
  const token = await api('POST', '/auth/tokens', { name: 'upload', expires_in_days: 1 });
  const media = await json<any>(await upload({ bearer: token.token }, sets.find((s: any) => s.name === 'audiobooks').id, 'test-audio-e2e.mp3'));
  expect((await api('GET', `/media/${media.id}`)).media).toMatchObject({ file_name: 'test-audio-e2e.mp3', type: 'audio' });
  expect(sandbox.sql("SELECT count(*) FROM media WHERE file_name='test-audio-e2e.mp3'")).toBe('1');
  await scan(cookie);
  await browserLogin(page, cookie);
  await page.goto('/');
  await page.keyboard.press('/');
  await page.locator('#search-input').fill('test-audio-e2e');
  await expect(page.locator('.media-card').filter({ hasText: 'test-audio-e2e' })).toBeVisible();
  await api('DELETE', `/media/${media.id}`);
  expect((await call('DELETE', `/api/v1/auth/tokens/${token.id}`, { cookie })).status).toBe(204);
});
