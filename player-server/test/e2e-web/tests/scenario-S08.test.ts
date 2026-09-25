import { test, expect, admin } from './helpers/scenario-fixture';
import { call } from './helpers/scenario-http';
test('S08: thumbnail regeneration, playback hints, stream and attachment download', async () => {
  const { cookie, api, media } = await admin(); const item = media.find((m: any) => m.type === 'video');
  const thumbnail = await call('GET', `/api/v1/media/${item.id}/thumbnail`, { cookie });
  expect(thumbnail.status).toBe(200); expect(thumbnail.headers.get('content-type')).toMatch(/^image\//);
  expect(await api('POST', `/media/${item.id}/thumbnail`)).toEqual({ status: 'ok' });
  expect(typeof (await api('GET', `/media/${item.id}/playback`)).needs_transcode).toBe('boolean');
  const stream = await call('GET', `/api/v1/media/${item.id}/stream`, { cookie, headers: { Range: 'bytes=0-1023' } });
  expect(stream.status).toBe(206); expect(stream.headers.get('content-type')).toMatch(/^video\//);
  expect((await stream.arrayBuffer()).byteLength).toBe(1024);
  const download = await call('GET', `/api/v1/media/${item.id}/download`, { cookie });
  expect(download.status).toBe(200); expect(download.headers.get('content-disposition')).toContain('attachment');
});
