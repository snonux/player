import { test, expect, type Page } from '@playwright/test';
import { bootstrap, triggerRescan, waitForServer } from './helpers/server';

let adminCookie: string;
let setId: number;

test.beforeAll(async () => {
  test.setTimeout(70_000);
  await waitForServer(15_000);
  adminCookie = await bootstrap();
  await triggerRescan(adminCookie, 30_000);

  // The scanner may still be running after the first set appears.
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    const baseURL = process.env.PLAYER_URL || 'http://localhost:8080';
    const sets = await (await fetch(`${baseURL}/api/sets`, { headers: { Cookie: adminCookie } })).json() as Array<{ id: number }>;
    for (const set of sets || []) {
      const browse = await (await fetch(`${baseURL}/api/sets/${set.id}/browse`, { headers: { Cookie: adminCookie } })).json() as { media?: Array<{ type: string }> };
      if ((browse.media || []).filter(media => media.type === 'image').length >= 2) {
        setId = set.id;
        return;
      }
    }
    await new Promise(resolve => setTimeout(resolve, 500));
  }
  throw new Error('Need a set with at least two media items for notes button tests');
});

async function openMedia(page: Page, view: 'browse' | 'filtered') {
  const origin = new URL(process.env.PLAYER_URL || 'http://localhost:8080');
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.goto('/');
  await page.locator(`.set-row[data-id="${setId}"]`).waitFor({ state: 'attached' });
  await page.evaluate(async ({ id, filtered }) => {
    const setsPath = '/js/views/sets.js';
    const { selectSet } = await import(setsPath);
    if (filtered) {
      const statePath = '/js/state.js';
      const { state } = await import(statePath);
      state.filters.type = 'image';
    }
    selectSet(id);
  }, { id: setId, filtered: view === 'filtered' });
  await expect.poll(() => page.locator('#media-grid .media-card').count()).toBeGreaterThanOrEqual(2);
}

// Both active render paths use media cards. The legacy .media-row selector has
// no renderer or list-mode toggle in the current UI.
for (const view of ['browse', 'filtered'] as const) {
  for (const priorSelection of [false, true]) {
    test(`${view} notes button opens and saves clicked item ${priorSelection ? 'after another selection' : 'without a selection'}`, async ({ page }) => {
      await openMedia(page, view);
      const cards = page.locator('#media-grid .media-card');
      const firstId = await cards.nth(0).getAttribute('data-id');
      const secondId = await cards.nth(1).getAttribute('data-id');
      expect(firstId).toBeTruthy();
      expect(secondId).toBeTruthy();

      const target = priorSelection ? cards.nth(1) : cards.nth(0);
      const targetId = priorSelection ? secondId : firstId;
      if (priorSelection) await cards.nth(0).click();

      const noteRequests: string[] = [];
      await page.route(/\/api\/media\/\d+\/notes$/, async route => {
        const id = new URL(route.request().url()).pathname.split('/')[3];
        noteRequests.push(`${route.request().method()} ${id}`);
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ content: `note for ${id}` }) });
      });

      await target.locator('[data-action="notes"]').click({ force: true });
      await expect(page.locator('#notes-modal')).toHaveClass(/open/);
      await expect(page.locator('#notes-textarea')).toHaveValue(`note for ${targetId}`);
      await expect(target).toHaveClass(/selected/);
      await page.locator('#notes-textarea').fill('updated note');
      await page.locator('#notes-save').click();
      await expect(page.locator('#notes-modal')).not.toHaveClass(/open/);
      expect(noteRequests).toEqual([`GET ${targetId}`, `POST ${targetId}`]);
    });
  }
}
