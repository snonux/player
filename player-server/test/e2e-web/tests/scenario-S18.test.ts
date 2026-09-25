import { test, expect, admin } from './helpers/scenario-fixture';
import { call } from './helpers/scenario-http';
test('S18: accumulated playback controls in-progress and validates negative requests', async () => {
  const { cookie, api, media } = await admin(); const [a, b] = media.filter((m: any) => m.type === 'audio');
  for (const [item, positions] of [[a, [12.5, 24, 36, 48, 60, 72]], [b, [90, 102, 114, 126, 138, 150]]] as const) {
    for (const position_seconds of positions) expect(await api('POST', '/progress', { media_id: item.id, position_seconds })).toEqual({ status: 'ok' });
  }
  expect((await api('GET', `/media/${a.id}`)).progress.position_seconds).toBe(72);
  expect(await api('GET', '/in-progress')).toEqual(expect.arrayContaining([expect.objectContaining({ id: a.id }), expect.objectContaining({ id: b.id })]));
  for (const body of [{}, { media_id: 0, position_seconds: 5 }]) {
    const res = await call('POST', '/api/v1/progress', { cookie, body }); expect(res.status).toBe(400); expect(await res.text()).toContain('media_id required');
  }
  expect((await call('POST', '/api/v1/progress', { cookie, body: { media_id: 999999999, position_seconds: 1 } })).status).toBe(404);
  const malformed = await call('POST', '/api/v1/progress', { cookie, raw: 'not-json', headers: { 'Content-Type': 'application/json' } }); expect(malformed.status).toBe(400); expect(await malformed.text()).toContain('invalid body');
  await api('POST', '/progress/status', { media_id: a.id, status: 'finished' });
  const remaining = await api('GET', '/in-progress'); expect(remaining.some((m: any) => m.id === a.id)).toBe(false); expect(remaining.some((m: any) => m.id === b.id)).toBe(true);
  for (const item of [a, b]) await api('POST', '/progress/status', { media_id: item.id, status: 'not_started' });
});
