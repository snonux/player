import { test, expect, admin, createUser } from './helpers/scenario-fixture';
import { call, json } from './helpers/scenario-http';
test('S19: viewer and owner permissions isolate two sets and enforce writes', async () => {
  const { cookie, api, media, sets } = await admin();
  const a = media.find((m: any) => m.set_id === sets.find((s: any) => s.name === 'images').id); const b = media.find((m: any) => m.type === 'video'); expect(a.set_id).not.toBe(b.set_id);
  const viewer = await createUser(cookie, 'viewer'); const owner = await createUser(cookie, 'owner');
  const grants = [{ user_id: viewer.id, set_id: a.set_id, role: 'viewer' }, { user_id: owner.id, set_id: b.set_id, role: 'owner' }];
  for (const grant of grants) await api('POST', '/admin/permissions', grant);
  expect((await api('GET', '/admin/permissions')).permissions).toEqual(expect.arrayContaining(grants.map(g => expect.objectContaining(g))));
  for (const [user, allowed, forbidden] of [[viewer, a, b], [owner, b, a]]) {
    const auth = { cookie: user.cookie };
    const sets = await json<any[]>(await call('GET', '/api/v1/sets', auth)); expect(sets.map(s => s.id)).toEqual([allowed.set_id]);
    const browse = await json<any>(await call('GET', `/api/v1/sets/${allowed.set_id}/browse`, auth)); expect(browse.media.some((m: any) => m.id === allowed.id)).toBe(true);
    expect((await call('GET', `/api/v1/sets/${forbidden.set_id}/browse`, auth)).status).toBe(403);
    expect((await call('GET', `/api/v1/media/${forbidden.id}`, auth)).status).toBe(403);
    expect((await call('GET', `/api/v1/media/${allowed.id}`, auth)).status).toBe(200);
    expect((await call('DELETE', `/api/v1/media/${forbidden.id}`, auth)).status).toBe(403);
  }
  expect((await call('POST', `/api/v1/media/${a.id}/tags`, { cookie: viewer.cookie, body: { tag: 'forbidden' } })).status).toBe(403);
  expect((await call('DELETE', `/api/v1/media/${a.id}`, { cookie: viewer.cookie })).status).toBe(403);
  expect(await json(await call('POST', `/api/v1/media/${a.id}/favorite`, { cookie: viewer.cookie }))).toEqual({ favorite: true });
  expect(await json(await call('POST', `/api/v1/media/${a.id}/notes`, { cookie: viewer.cookie, body: { content: 'viewer note' } }))).toMatchObject({ content: 'viewer note' });
  expect((await call('DELETE', `/api/v1/media/${b.id}`, { cookie: owner.cookie })).status).toBe(200);
  await api('POST', `/media/${b.id}/restore`);
  for (const grant of grants) await api('DELETE', '/admin/permissions', grant);
  for (const user of [viewer, owner]) await api('DELETE', `/admin/users/${user.id}`);
});
