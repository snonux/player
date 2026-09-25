import { test, expect, admin } from './helpers/scenario-fixture';
import { call } from './helpers/scenario-http';
test('S20: HEAD, exact byte ranges, multipart and cache validators', async () => {
  const { cookie, media } = await admin(); const item = media.find((m: any) => m.type === 'audio');
  const route = `/api/v1/media/${item.id}`;
  const head = await call('HEAD', `${route}/stream`, { cookie });
  expect(head.status).toBe(200); const size = Number(head.headers.get('content-length')); expect(size).toBeGreaterThan(300);
  expect(head.headers.get('accept-ranges')).toBe('bytes'); expect(head.headers.get('content-type')).toMatch(/^audio\//); expect((await head.arrayBuffer()).byteLength).toBe(0);
  const download = await call('HEAD', `${route}/download`, { cookie }); expect(download.status).toBe(200); expect(download.headers.get('content-disposition')).toContain('attachment'); expect(Number(download.headers.get('content-length'))).toBe(size); expect((await download.arrayBuffer()).byteLength).toBe(0);
  const thumb = await call('HEAD', `${route}/thumbnail`, { cookie }); expect(thumb.status).toBe(200); expect(thumb.headers.get('content-type')).toMatch(/^image\//); expect(thumb.headers.get('cache-control')).toBe('no-cache'); expect((await thumb.arrayBuffer()).byteLength).toBe(0);
  const full = await call('GET', `${route}/stream`, { cookie }); expect(full.status).toBe(200); const bytes = Buffer.from(await full.arrayBuffer()); expect(bytes.length).toBe(size);
  for (const [range, start, end] of [['0-99', 0, 99], ['100-199', 100, 199], ['-100', size-100, size-1], ['100-', 100, size-1]] as const) {
    const response = await call('GET', `${route}/stream`, { cookie, headers: { Range: `bytes=${range}` } });
    expect(response.status).toBe(206); expect(response.headers.get('content-range')).toBe(`bytes ${start}-${end}/${size}`);
    expect(Number(response.headers.get('content-length'))).toBe(end-start+1); expect(Buffer.from(await response.arrayBuffer())).toEqual(bytes.subarray(start, end+1));
  }
  const unsatisfied = await call('GET', `${route}/stream`, { cookie, headers: { Range: 'bytes=999999999-' } }); expect(unsatisfied.status).toBe(416); expect(unsatisfied.headers.get('content-range')).toBe(`bytes */${size}`);
  const malformed = await call('GET', `${route}/stream`, { cookie, headers: { Range: 'bytes=garbage' } }); expect(malformed.status).toBe(416); expect(await malformed.text()).toContain('invalid range');
  const multi = await call('GET', `${route}/stream`, { cookie, headers: { Range: 'bytes=0-99,200-299' } }); expect(multi.status).toBe(206); expect(multi.headers.get('content-type')).toMatch(/^multipart\/byteranges; boundary=/); expect((await multi.arrayBuffer()).byteLength).toBeGreaterThan(200);
  const modified = full.headers.get('last-modified'); const etag = full.headers.get('etag'); expect(modified).toBeTruthy(); expect(etag).toMatch(/^"[^"\s]+"$/);
  for (const headers of [{ 'If-Modified-Since': modified! }, { 'If-None-Match': etag! }] as Record<string, string>[]) {
    const cached = await call('GET', `${route}/stream`, { cookie, headers }); expect(cached.status).toBe(304); expect((await cached.arrayBuffer()).byteLength).toBe(0);
  }
  for (const method of ['GET', 'HEAD']) expect((await call(method, '/api/v1/media/999999999/stream', { cookie })).status).toBe(404);
});
