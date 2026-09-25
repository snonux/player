import { test, expect, admin } from './helpers/scenario-fixture';
import { call } from './helpers/scenario-http';
test('S07: favorite, tag, and note mutations persist and can be removed', async () => {
  const { cookie, api, media } = await admin(); const id = media[0].id;
  expect(await api('POST', `/media/${id}/favorite`)).toEqual({ favorite: true });
  expect(await api('POST', `/media/${id}/favorite`)).toEqual({ favorite: false });
  const before = await api('GET', '/tags'); expect(Array.isArray(before)).toBe(true);
  expect(await api('POST', `/media/${id}/tags`, { tag: 'e2e-test' })).toEqual({ status: 'ok' });
  expect(await api('GET', '/tags')).toEqual(expect.arrayContaining([expect.objectContaining({ name: 'e2e-test' })]));
  await api('DELETE', `/media/${id}/tags/e2e-test`);
  expect((await api('GET', `/media/${id}`)).tags).toEqual([]);
  expect((await call('GET', `/api/v1/media/${id}/notes`, { cookie })).status).toBe(204);
  expect(await api('POST', `/media/${id}/notes`, { content: 'e2e note' })).toMatchObject({ media_id: id, content: 'e2e note' });
  expect(await api('GET', `/media/${id}/notes`)).toMatchObject({ content: 'e2e note' });
  await api('DELETE', `/media/${id}/notes`);
  expect((await call('GET', `/api/v1/media/${id}/notes`, { cookie })).status).toBe(204);
});
