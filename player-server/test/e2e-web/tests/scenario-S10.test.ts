import { test, expect, admin } from './helpers/scenario-fixture';
test('S10: batch progress persists both positions and status transitions', async ({ sandbox }) => {
  const { api, media } = await admin(); const [a, b] = media.filter((m: any) => m.type === 'audio');
  expect(await api('POST', '/progress/batch', { updates: [{ media_id: a.id, position_seconds: 30 }, { media_id: b.id, position_seconds: 60 }] })).toEqual({ status: 'ok' });
  for (const [item, position] of [[a, 30], [b, 60]]) expect((await api('GET', `/media/${item.id}`)).progress.position_seconds).toBe(position);
  expect(sandbox.sql(`SELECT position_seconds FROM playback_progress WHERE media_id=${a.id}`)).toBe('30.0');
  await api('POST', '/progress/status', { media_id: a.id, status: 'finished' });
  expect((await api('GET', `/media/${a.id}`)).progress.finished).toBe(true);
  await api('POST', '/progress/status', { media_id: a.id, status: 'not_started' });
  expect(await api('GET', `/media/${a.id}`)).not.toHaveProperty('progress');
  expect(Array.isArray(await api('GET', '/in-progress'))).toBe(true);
});
