import { test, expect, admin, createUser } from './helpers/scenario-fixture';
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
