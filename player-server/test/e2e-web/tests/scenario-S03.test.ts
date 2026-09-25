import { readFileSync } from 'node:fs';
import path from 'node:path';
import { test, expect, admin, upload, scan, browserLogin } from './helpers/scenario-fixture';
import { json, call } from './helpers/scenario-http';
test('S03: Bearer upload survives rescan and appears in browser search', async ({ page, sandbox }) => {
  const { cookie, api, sets } = await admin();
  const token = await api('POST', '/auth/tokens', { name: 'upload', expires_in_days: 1 });
  const media = await json<any>(await upload({ bearer: token.token }, sets.find((s: any) => s.name === 'audiobooks').id, 'test-audio-e2e.mp3'));
  expect((await api('GET', `/media/${media.id}`)).media).toMatchObject({ file_name: 'test-audio-e2e.mp3', type: 'audio' });
  expect(sandbox.sql("SELECT count(*) FROM media WHERE file_name='test-audio-e2e.mp3'")).toBe('1');
  await scan(cookie);
  await browserLogin(page, cookie);
  await page.goto('/');
  await page.keyboard.press('/');
  await page.locator('#search-input').fill('test-audio-e2e');
  await expect(page.locator('.media-card').filter({ hasText: 'test-audio-e2e' })).toBeVisible();
  await api('DELETE', `/media/${media.id}`);
  expect((await call('DELETE', `/api/v1/auth/tokens/${token.id}`, { cookie })).status).toBe(204);
});

test('S03: keyboard upload dialog adds a file to the current set', async ({ page }) => {
  const { cookie, sets } = await admin();
  await browserLogin(page, cookie);
  await page.goto('/');
  const set = sets.find((s: any) => s.name === 'audiobooks');
  await page.getByRole('button', { name: `Set ${set.name}`, exact: true }).click();
  await expect(page.locator('#breadcrumb-bar')).toContainText(set.name);

  // u opens the dialog with focus on the file picker.
  await page.keyboard.press('u');
  const modal = page.locator('#upload-modal');
  await expect(modal).toHaveClass(/open/);
  await expect(page.locator('#upload-file')).toBeFocused();
  const bytes = readFileSync(path.join(__dirname, '../../../testdata/media/audiobooks/aesops-fables/37-chapter.mp3'));
  await page.locator('#upload-file').setInputFiles({ name: 'browser-upload.mp3', mimeType: 'audio/mpeg', buffer: bytes.subarray(0, 900_000) });
  await page.getByRole('button', { name: 'Upload to current set' }).click();
  await expect(page.locator('#toast')).toContainText('Upload complete');
  await expect(modal).not.toHaveClass(/open/);
  await expect(page.locator('#media-grid')).toContainText('browser-upload.mp3');
});
