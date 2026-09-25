import { test, expect, admin, createUser, podcast } from './helpers/scenario-fixture';
import { call, json } from './helpers/scenario-http';
test('S23: deletion cascades populated user rows and invalidates all credentials and shares', async ({ sandbox }) => {
  const { cookie, api, media } = await admin(); const item = media.find((m: any) => m.type === 'video'); const user = await createUser(cookie, 'e2e-cascade-user');
  // Owner is required to create the global tag; personal data is available to viewers too (S19).
  await api('POST', '/admin/permissions', { user_id: user.id, set_id: item.set_id, role: 'owner' });
  const own = async (method: string, route: string, body?: unknown) => json<any>(await call(method, `/api/v1${route}`, { cookie: user.cookie, body }));
  const bearer = await own('POST', '/auth/tokens', { name: 'cascade-a', expires_in_days: 1 });
  await own('POST', '/auth/tokens', { name: 'cascade-b', expires_in_days: 1 });
  await own('POST', `/media/${item.id}/tags`, { tag: 'e2e-cascade-tag' });
  expect(await own('POST', `/media/${item.id}/favorite`)).toEqual({ favorite: true });
  expect(await own('POST', `/media/${item.id}/notes`, { content: 'e2e cascade note' })).toMatchObject({ content: 'e2e cascade note', media_id: item.id });
  await own('POST', '/progress', { media_id: item.id, position_seconds: 15 });
  const share = await own('POST', `/media/${item.id}/shares`);
  await podcast(cookie, async feed => {
    await api('POST', '/admin/permissions', { user_id: user.id, set_id: feed.set_id, role: 'viewer' });
    const episodes = await own('GET', `/podcasts/${feed.set_id}/episodes`);
    expect((await call('POST', `/api/v1/podcasts/episodes/${episodes[0].id}/complete`, { cookie: user.cookie })).status).toBe(204);
    const predicates: Record<string, string> = {};
    for (const table of ['api_tokens', 'sessions', 'set_permissions', 'favorites', 'media_notes', 'playback_progress', 'podcast_status']) predicates[table] = `user_id=${user.id}`;
    predicates.shares = `created_by=${user.id}`;
    predicates.playback_accumulator = `session_id='${user.cookie.slice('session='.length)}'`;
    for (const [table, predicate] of Object.entries(predicates)) expect(Number(sandbox.sql(`SELECT count(*) FROM ${table} WHERE ${predicate}`)), `${table} setup`).toBeGreaterThan(0);
    expect(sandbox.sql(`SELECT count(*) FROM api_tokens WHERE user_id=${user.id}`)).toBe('2');
    await api('DELETE', `/admin/users/${user.id}`);
    for (const [table, predicate] of Object.entries(predicates)) expect(sandbox.sql(`SELECT count(*) FROM ${table} WHERE ${predicate}`), `${table} cascade`).toBe('0');
    expect(sandbox.sql("SELECT count(*) FROM tags WHERE name='e2e-cascade-tag'")).toBe('1');
  });
  expect((await api('GET', '/admin/users')).some((u: any) => u.id === user.id)).toBe(false);
  expect((await call('GET', '/api/v1/auth/tokens', { cookie: user.cookie })).status).toBe(401);
  expect((await call('GET', '/api/v1/media', { bearer: bearer.token })).status).toBe(401);
  expect((await call('POST', '/api/v1/auth/login', { body: { username: user.username, password: 'TestPassw0rd!' } })).status).toBe(401);
  expect((await call('GET', `/s/${share.token}/thumbnail`)).status).toBe(404);
});
