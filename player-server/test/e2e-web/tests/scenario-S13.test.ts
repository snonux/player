import { test, expect, admin, createUser } from './helpers/scenario-fixture';
test('S13: permission matrix reflects viewer grant and revoke', async () => {
  const { cookie, api } = await admin(); const user = await createUser(cookie);
  const before = await api('GET', '/admin/permissions');
  expect(before.users).toEqual(expect.arrayContaining([expect.objectContaining({ id: user.id })]));
  const grant = { user_id: user.id, set_id: before.sets[0].id, role: 'viewer' };
  await api('POST', '/admin/permissions', grant);
  const after = await api('GET', '/admin/permissions');
  expect(after.permissions).toHaveLength(before.permissions.length + 1);
  expect(after.permissions).toEqual(expect.arrayContaining([expect.objectContaining(grant)]));
  await api('DELETE', '/admin/permissions', grant);
  expect((await api('GET', '/admin/permissions')).permissions).toEqual(before.permissions);
  await api('DELETE', `/admin/users/${user.id}`);
});
