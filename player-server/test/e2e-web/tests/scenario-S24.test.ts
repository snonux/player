import { test, expect, admin, upload, scan } from './helpers/scenario-fixture';
import { call, json } from './helpers/scenario-http';
import { existsSync, unlinkSync } from 'node:fs';
import path from 'node:path';
test('S24: rescans preserve trash and reconcile missing files without duplicate rows', async ({ sandbox }) => {
  const { cookie, api, sets } = await admin(); const set = sets.find((s: any) => s.name === 'audiobooks');
  const token = await api('POST', '/auth/tokens', { name: 'rescan', expires_in_days: 1 });
  const item = await json<any>(await upload({ bearer: token.token }, set.id, 'test-soft-delete-rescan.mp3'));
  expect((await api('GET', '/media')).some((m: any) => m.id === item.id)).toBe(true);
  await api('DELETE', `/media/${item.id}`);
  const deleted = sandbox.sql(`SELECT deleted_at FROM media WHERE id=${item.id}`); expect(deleted).toBeTruthy();
  expect(existsSync(item.abs_path)).toBe(true); await scan(cookie);
  expect(sandbox.sql(`SELECT deleted_at FROM media WHERE id=${item.id}`)).toBe(deleted);
  expect(sandbox.sql(`SELECT count(*) FROM media WHERE set_id=${set.id} AND rel_path='test-soft-delete-rescan.mp3'`)).toBe('1');
  const orphan = await json<any>(await upload({ bearer: token.token }, set.id, 'test-disk-deleted.mp3'));
  expect(orphan.abs_path).toBe(path.join(sandbox.mediaRoot, set.root_path, 'test-disk-deleted.mp3'));
  unlinkSync(orphan.abs_path); expect(existsSync(orphan.abs_path)).toBe(false); await scan(cookie);
  expect(sandbox.sql(`SELECT count(*) FROM media WHERE id=${orphan.id} AND deleted_at IS NOT NULL`)).toBe('1');
  for (const id of [item.id, orphan.id]) {
    expect((await api('GET', '/media')).some((m: any) => m.id === id)).toBe(false);
    expect((await api('GET', '/admin/trash')).some((m: any) => m.id === id)).toBe(true);
  }
  unlinkSync(item.abs_path);
  expect((await call('DELETE', `/api/v1/auth/tokens/${token.id}`, { cookie })).status).toBe(204);
});
