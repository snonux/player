import { test, expect, admin } from './helpers/scenario-fixture';
import { call } from './helpers/scenario-http';
test('S11: owner share listing and public file endpoints honor revocation', async () => {
  const { api, media } = await admin(); const item = media.find((m: any) => m.type === 'video');
  const { token } = await api('POST', `/media/${item.id}/shares`);
  expect(await api('GET', '/shares')).toEqual(expect.arrayContaining([expect.objectContaining({ token })]));
  const thumb = await call('GET', `/s/${token}/thumbnail`);
  expect(thumb.status).toBe(200); expect(thumb.headers.get('content-type')).toMatch(/^image\//);
  const download = await call('GET', `/s/${token}/download`);
  expect(download.status).toBe(200); expect(download.headers.get('content-disposition')).toMatch(/^attachment/);
  await api('DELETE', `/shares/${token}`);
  expect(await api('GET', '/shares')).not.toEqual(expect.arrayContaining([expect.objectContaining({ token })]));
  expect((await call('GET', `/s/${token}/thumbnail`)).status).toBe(404);
});
