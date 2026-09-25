import { test, expect, admin, createUser } from './helpers/scenario-fixture';
import { unlinkSync } from 'node:fs';
import { call } from './helpers/scenario-http';
test('S15: unauthenticated, nonadmin, missing-resource and empty-cover boundaries', async ({ sandbox }) => {
  for (const route of ['/media', '/sets', '/tags']) {
    const res = await call('GET', `/api/v1${route}`); expect(res.status).toBe(401); expect(await res.text()).toContain('unauthorized');
  }
  expect((await call('POST', '/api/v1/auth/tokens', { body: { name: 'forbidden' } })).status).toBe(401);
  const { cookie, api, media, sets } = await admin(); const user = await createUser(cookie);
  for (const route of ['/admin/users', '/admin/trash', '/admin/permissions', '/admin/scan-progress']) expect((await call('GET', `/api/v1${route}`, { cookie: user.cookie })).status).toBe(403);
  expect((await call('POST', '/api/v1/admin/rescan', { cookie: user.cookie })).status).toBe(403);
  for (const [method, route] of [['GET', '/media/999999999'], ['GET', '/sets/999999999/browse'], ['DELETE', '/shares/missing'], ['DELETE', `/media/${media[0].id}/tags/missing`]]) expect((await call(method, `/api/v1${route}`, { cookie })).status).toBe(404);
  expect((await call('GET', '/s/missing/thumbnail')).status).toBe(404);
  const audio = media.find((m: any) => m.type === 'audio');
  expect(audio.thumbnail_path.startsWith(`${sandbox.mediaRoot}/`)).toBe(true);
  unlinkSync(audio.thumbnail_path);
  expect((await call('GET', `/api/v1/media/${audio.id}/thumbnail`, { cookie })).status).toBe(404);
  const empty = sets.find((s: any) => s.name === 'empty'); expect(empty).toBeTruthy();
  expect((await call('POST', `/api/v1/sets/${empty.id}/cover`, { cookie })).status).toBe(400);
  await api('DELETE', `/admin/users/${user.id}`);
  expect((await api('GET', '/admin/users')).some((u: any) => u.id === user.id)).toBe(false);
});
