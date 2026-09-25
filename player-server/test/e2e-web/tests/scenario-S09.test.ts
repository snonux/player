import { test, expect, admin, upload } from './helpers/scenario-fixture';
import { call, json } from './helpers/scenario-http';
test('S09: uploaded media leaves active list, enters trash, and restores', async () => {
  const { cookie, api, sets } = await admin();
  const token = await api('POST', '/auth/tokens', { name: 'delete-restore', expires_in_days: 1 });
  const item = await json<any>(await upload({ bearer: token.token }, sets[0].id, 'test-delete-restore.mp3'));
  const contains = (items: any[]) => items.some(m => m.id === item.id);
  expect(contains(await api('GET', '/media'))).toBe(true);
  await api('DELETE', `/media/${item.id}`);
  expect(contains(await api('GET', '/media'))).toBe(false);
  expect(contains(await api('GET', '/admin/trash'))).toBe(true);
  await api('POST', `/media/${item.id}/restore`);
  expect(contains(await api('GET', '/media'))).toBe(true);
  expect(contains(await api('GET', '/admin/trash'))).toBe(false);
  await api('DELETE', `/media/${item.id}`);
  expect((await call('DELETE', `/api/v1/auth/tokens/${token.id}`, { cookie })).status).toBe(204);
});
