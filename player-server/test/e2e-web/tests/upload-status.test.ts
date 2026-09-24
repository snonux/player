import { test, expect } from '@playwright/test';
import { bootstrap, triggerRescan, waitForServer } from './helpers/server';

test.use({ serviceWorkers: 'block' });

let adminCookie: string;

test.beforeAll(async () => {
  test.setTimeout(100_000);
  await waitForServer();
  adminCookie = await bootstrap();
  await triggerRescan(adminCookie);
});

test('upload is single-flight and can retry the same file after failure', async ({ page }) => {
  const origin = new URL(process.env.PLAYER_URL || 'http://localhost:8080');
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.goto('/');
  const setRow = page.locator('#set-list .set-row').first();
  await setRow.waitFor({ state: 'attached' });
  const setId = Number(await setRow.getAttribute('data-id'));
  await page.evaluate(async id => {
    const statePath = '/js/state.js';
    const uploadPath = '/js/views/upload.js';
    (await import(statePath)).state.selectedSetId = id;
    (await import(uploadPath)).showUpload();
  }, setId);

  const modal = page.locator('#upload-modal');
  const form = page.locator('#upload-form');
  const fileInput = page.locator('#upload-file');
  const submit = page.locator('#upload-submit');
  const close = page.locator('#upload-close');
  await fileInput.setInputFiles({ name: 'sample.mp3', mimeType: 'audio/mpeg', buffer: Buffer.from('test audio') });

  let firstStarted!: () => void;
  const started = new Promise<void>(resolve => { firstStarted = resolve; });
  let releaseFirst!: () => void;
  const firstResponse = new Promise<void>(resolve => { releaseFirst = resolve; });
  const requests: string[] = [];
  await page.route(/\/api\/sets\/\d+\/upload$/, async route => {
    requests.push(route.request().url());
    if (requests.length === 1) {
      firstStarted();
      await firstResponse;
      await route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"temporary failure"}' });
      return;
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
  });

  await submit.click();
  await started;
  await expect(submit).toBeDisabled();
  await expect(fileInput).toBeDisabled();
  await expect(close).toBeDisabled();
  await expect(form).toHaveAttribute('aria-busy', 'true');
  await expect(page.locator('#upload-status')).toBeVisible();
  await expect(page.locator('#upload-progress')).toBeVisible();
  await modal.click({ position: { x: 5, y: 5 } });
  await page.keyboard.press('Escape');
  await expect(modal).toHaveClass(/open/);
  await form.dispatchEvent('submit');
  expect(requests).toHaveLength(1);

  await page.evaluate(async () => {
    const statePath = '/js/state.js';
    (await import(statePath)).state.selectedSetId = 9999;
  });
  releaseFirst();
  await expect(submit).toBeEnabled();
  await expect(fileInput).toBeEnabled();
  await expect(close).toBeEnabled();
  await expect(form).toHaveAttribute('aria-busy', 'false');
  await expect(page.locator('#upload-status-text')).toHaveText('Upload failed. Try again.');
  await expect(modal).toHaveClass(/open/);
  expect(await fileInput.evaluate((input: any) => input.files.length)).toBe(1);
  expect(new URL(requests[0]).pathname).toBe(`/api/sets/${setId}/upload`);

  await page.evaluate(async id => {
    const statePath = '/js/state.js';
    (await import(statePath)).state.selectedSetId = id;
  }, setId);
  await submit.click();
  await expect(modal).not.toHaveClass(/open/);
  expect(requests).toHaveLength(2);
  expect(new URL(requests[1]).pathname).toBe(`/api/sets/${setId}/upload`);
  expect(await fileInput.evaluate((input: any) => input.files.length)).toBe(0);
});
