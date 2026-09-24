import { test, expect, type Page, type Response } from '@playwright/test';
import { bootstrap, triggerRescan, waitForServer } from './helpers/server';

const baseURL = process.env.PLAYER_URL || 'http://localhost:8080';
let adminCookie: string;
let setId: number;

test.beforeAll(async () => {
  test.setTimeout(100_000);
  await waitForServer();
  adminCookie = await bootstrap();
  await triggerRescan(adminCookie);
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    const sets = await (await fetch(`${baseURL}/api/sets`, { headers: { Cookie: adminCookie } })).json() as Array<{ id: number }>;
    for (const set of sets) {
      const browse = await (await fetch(`${baseURL}/api/sets/${set.id}/browse`, { headers: { Cookie: adminCookie } })).json() as { media?: Array<unknown> };
      if (browse.media?.length) {
        setId = set.id;
        return;
      }
    }
    await new Promise(resolve => setTimeout(resolve, 500));
  }
  throw new Error('Need a set with media at its root for progress status tests');
});

async function openMediaCard(page: Page) {
  const origin = new URL(baseURL);
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.goto('/');
  await page.locator(`.set-row[data-id="${setId}"]`).waitFor({ state: 'attached' });
  await page.evaluate(async (id) => {
    const setsPath = '/js/views/sets.js';
    const { selectSet } = await import(setsPath);
    selectSet(id);
  }, setId);
  const card = page.locator('#media-grid .media-card').first();
  await expect(card).toBeVisible();
  const id = Number(await card.getAttribute('data-id'));
  expect(Number.isSafeInteger(id) && id > 0).toBe(true);
  return { card, id };
}

async function expectStatusRequest(page: Page, click: () => Promise<void>, id: number, status: string) {
  const responsePromise = page.waitForResponse(response =>
    response.url().endsWith('/api/progress/status') && response.request().method() === 'POST');
  await click();
  const response: Response = await responsePromise;
  expect(response.status(), await response.text()).toBe(200);
  expect(response.request().postDataJSON()).toEqual({ media_id: id, status });
  const detail = await (await page.request.get(`/api/media/${id}`)).json() as { progress?: { finished: boolean } | null };
  expect(detail.progress?.finished ?? false).toBe(status === 'finished');
  if (status === 'not_started') expect(detail.progress).toBeFalsy();
}

test('grid progress buttons send numeric IDs to the live handler', async ({ page }) => {
  const { card, id } = await openMediaCard(page);
  await expectStatusRequest(page, () => card.locator('[data-action="mark-finished"]').click(), id, 'finished');
  await expectStatusRequest(page, () => card.locator('[data-action="mark-not-started"]').click(), id, 'not_started');
});

test('media info progress buttons send numeric IDs to the live handler', async ({ page }) => {
  const { card, id } = await openMediaCard(page);
  await card.click();
  await page.keyboard.press('i');
  await expect(page.locator('#media-info-modal')).toHaveClass(/open/);
  await expect(page.locator('#media-info-body [data-media-info-action="mark-finished"]')).toBeVisible();
  await expectStatusRequest(page, () => page.locator('[data-media-info-action="mark-finished"]').click(), id, 'finished');
  await expectStatusRequest(page, () => page.locator('[data-media-info-action="mark-not-started"]').click(), id, 'not_started');
});
