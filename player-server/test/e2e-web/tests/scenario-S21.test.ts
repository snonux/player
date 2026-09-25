import { test, expect, admin, upload, createUser } from './helpers/scenario-fixture';
import { call, json } from './helpers/scenario-http';
import { existsSync } from 'node:fs';
import path from 'node:path';
test('S21: upload validation, size limit, traversal confinement and deduplication', async ({ sandbox }) => {
  const { cookie, api, sets } = await admin(); const set = sets.find((s: any) => s.name === 'audiobooks'); const auth = { cookie };
  const route = `/api/v1/sets/${set.id}/upload`;
  const empty = await call('POST', route, auth); expect(empty.status).toBe(400); expect(await empty.text()).toContain('invalid multipart form');
  const form = new FormData(); form.append('note', 'hello'); const missing = await call('POST', route, { cookie, form }); expect(missing.status).toBe(400); expect(await missing.text()).toContain('missing file');
  for (const name of ['e2e-s21.txt', 'e2e-s21.mp3.txt']) {
    const response = await upload(auth, set.id, name, Buffer.from('hello')); expect(response.status).toBe(400); expect(await response.text()).toContain('unsupported file extension');
    expect(await api('GET', `/media?search=${name}`)).toEqual([]);
  }
  const traversal = await json<any>(await upload(auth, set.id, '../../../etc/passwd.mp3'));
  expect(traversal).toMatchObject({ file_name: 'passwd.mp3', rel_path: 'passwd.mp3' });
  expect(traversal.abs_path).toBe(path.join(sandbox.mediaRoot, set.root_path, 'passwd.mp3')); expect(existsSync(traversal.abs_path)).toBe(true);
  expect(sandbox.sql(`SELECT file_name || '|' || rel_path FROM media WHERE id=${traversal.id}`)).toBe('passwd.mp3|passwd.mp3');
  expect((await upload(auth, 999999999, 'e2e-s21-missing.mp3')).status).toBe(404); expect(await api('GET', '/media?search=e2e-s21-missing')).toEqual([]);
  const big = await upload(auth, set.id, 'e2e-s21-big.mp3', Buffer.alloc(1024*1024+65536)); expect(big.status).toBe(413); expect(await big.text()).toContain('file too large');
  const user = await createUser(cookie); expect((await upload({ cookie: user.cookie }, set.id, 'e2e-s21-noperm.mp3')).status).toBe(403); expect(await api('GET', '/media?search=e2e-s21-noperm')).toEqual([]);
  // mime/multipart classifies filename="" as a field, so FormFile reports missing file.
  const blank = await upload(auth, set.id, '', Buffer.from('hello')); expect(blank.status).toBe(400); expect(await blank.text()).toContain('missing file');
  const first = await json<any>(await upload(auth, set.id, 'e2e-s21-dedupe.mp3')); const second = await json<any>(await upload(auth, set.id, 'e2e-s21-dedupe.mp3'));
  expect(first.id).not.toBe(second.id); expect(first.file_name).toBe('e2e-s21-dedupe.mp3'); expect(second.file_name).toBe('e2e-s21-dedupe(1).mp3');
  expect((await api('GET', '/media?search=e2e-s21-dedupe')).map((m: any) => m.id).sort()).toEqual([first.id, second.id].sort());
  for (const item of [traversal, first, second]) await api('DELETE', `/media/${item.id}`);
  await api('DELETE', `/admin/users/${user.id}`);
  expect(await api('GET', '/media?search=e2e-s21-dedupe')).toEqual([]);
  expect((await api('GET', '/admin/users')).some((u: any) => u.id === user.id)).toBe(false);
});
