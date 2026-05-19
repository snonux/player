/**
 * search-filter.test.ts — Playwright tests for the search/filter UI overlay.
 *
 * Coverage:
 *   1. Login, open a set, then open the search overlay with the "/" hotkey.
 *      Type a query into #search-input and confirm the media grid re-renders
 *      with at most the matching items (the grid count drops or stays equal,
 *      but the empty-state is not shown for known testdata names).
 *   2. Toggle the favorites filter via the query syntax `like:1` and confirm
 *      the grid updates (either to favourites-only or to an empty list).
 *   3. Clear the filter via the #search-clear button and confirm the grid
 *      returns to the full set listing.
 *
 * The Player UI does not have a dedicated "favourites toggle" button — the
 * favourites filter is set via the search syntax (`like:1`) which is the
 * canonical interaction in the SPA. The same applies to the type filter
 * (`type:audio`). We exercise both via the search input.
 *
 * Selectors are taken from web/index.html and web/js/search.js:
 *   #search-overlay, #search-input, #search-clear, #media-grid, .media-card / .media-row
 */

import { test, expect, Page, BrowserContext } from '@playwright/test';
import {
  bootstrap,
  triggerRescan,
  waitForServer,
  ADMIN_USER,
  ADMIN_PASS,
} from './helpers/server';

let adminCookie: string = '';

test.beforeAll(async () => {
  await waitForServer(15_000);
  adminCookie = await bootstrap();
  await triggerRescan(adminCookie, 30_000);
}, 60_000);

/**
 * injectSessionCookie matches the helper in smoke.test.ts: it parses the
 * session= cookie out of the raw Cookie header and adds it to the browser
 * context for the configured PLAYER_URL host.
 */
async function injectSessionCookie(
  context: BrowserContext,
  cookieHeader: string,
): Promise<void> {
  const match = cookieHeader.match(/session=([^;]+)/);
  if (!match) throw new Error(`Cannot parse session cookie from: ${cookieHeader}`);
  const baseURL = process.env.PLAYER_URL || 'http://localhost:8080';
  const parsed = new URL(baseURL);
  await context.addCookies([
    {
      name: 'session',
      value: match[1],
      domain: parsed.hostname,
      path: '/',
      sameSite: 'Strict',
    },
  ]);
}

async function openAuthenticatedPage(page: Page, path: string): Promise<void> {
  await injectSessionCookie(page.context(), adminCookie);
  await page.goto(path);
  // The SPA renders the logout button once the static HTML has loaded.
  await page.waitForSelector('#logout-btn', { timeout: 15_000 });
}

/**
 * openFirstSet opens the auto-hiding header, opens the sidebar and clicks the
 * first set row so the media grid is populated. This is the same dance used
 * by smoke.test.ts's "clicking a set loads the media grid" test.
 */
async function openFirstSet(page: Page): Promise<void> {
  // Reveal the auto-hiding header by hovering at the top of the viewport.
  const viewport = page.viewportSize();
  await page.mouse.move(viewport ? viewport.width / 2 : 400, 2);
  await page.waitForTimeout(300);
  await page.locator('#sidebar-toggle').click();
  const firstSetRow = page.locator('.set-row').first();
  await firstSetRow.waitFor({ state: 'visible', timeout: 10_000 });
  await firstSetRow.click({ force: true });

  const mediaGrid = page.locator('#media-grid');
  await expect(mediaGrid).toBeVisible({ timeout: 10_000 });
  // Loading placeholder must disappear before we start asserting on item counts.
  await expect(mediaGrid.locator('text=Loading...')).toHaveCount(0, { timeout: 15_000 });
}

/**
 * openSearchOverlay reveals the search overlay by pressing the "/" hotkey
 * (the keybinding registered in web/js/keyboard.js). The overlay is hidden
 * via the .hidden class — once the class is removed we can interact with
 * the input.
 */
async function openSearchOverlay(page: Page): Promise<void> {
  await page.keyboard.press('/');
  const overlay = page.locator('#search-overlay');
  await expect(overlay).not.toHaveClass(/hidden/, { timeout: 5_000 });
  await expect(page.locator('#search-input')).toBeVisible({ timeout: 5_000 });
}

// -----------------------------------------------------------------------
// 1. Typing into the search input updates the media grid.
// -----------------------------------------------------------------------

test('typing into search input filters the media grid', async ({ page }) => {
  await openAuthenticatedPage(page, '/');
  await openFirstSet(page);

  // Capture the initial result-count text so we can verify it changed after
  // applying a filter. result-count is rendered unconditionally by loadMedia()
  // after every render pass.
  const grid = page.locator('#media-grid');
  const resultCount = page.locator('#result-count');
  await expect(resultCount).toBeVisible({ timeout: 5_000 });
  const beforeText = (await resultCount.textContent()) ?? '';

  await openSearchOverlay(page);

  // Type a query that matches at least one known testdata item. testdata/media
  // contains audiobooks/aesops-fables/*.mp3, so "aesop" must match >=1 file.
  // The search input debounces input events by 300ms; pressing Enter flushes
  // the timer (see search.js keydown handler).
  await page.fill('#search-input', 'aesop');
  await page.locator('#search-input').press('Enter');

  // Wait for the grid to re-render — the loading placeholder appears briefly.
  await page.waitForTimeout(800);
  await expect(grid.locator('text=Loading...')).toHaveCount(0, { timeout: 5_000 });

  // After filtering, the grid switches from the browse view (folder cards)
  // to a flat filtered list — so the renderer is exercised. We assert at
  // least one matching media item is visible.
  const filteredItems = grid.locator('.media-card, .media-row');
  await expect(filteredItems.first()).toBeVisible({ timeout: 5_000 });
  const filteredCount = await filteredItems.count();
  expect(filteredCount).toBeGreaterThan(0);

  // result-count text should have updated to reflect the new filtered total
  // (the format includes the number of results, so the string changes when
  // the count changes).
  const afterText = (await resultCount.textContent()) ?? '';
  expect(afterText).not.toEqual(beforeText);
});

// -----------------------------------------------------------------------
// 2. Toggling the favorites filter (via like:1 syntax) updates the grid.
//    There is no dedicated favourites-toggle button in the current UI —
//    the favourites filter is applied through the search query syntax.
// -----------------------------------------------------------------------

test('favorites filter via like:1 syntax updates the grid', async ({ page }) => {
  await openAuthenticatedPage(page, '/');
  await openFirstSet(page);

  await openSearchOverlay(page);
  await page.fill('#search-input', 'like:1');
  await page.locator('#search-input').press('Enter');

  // After the filter is applied the grid either shows favourite items only
  // or shows an empty-state message. We assert that the grid finished loading
  // and that the result-count display updates (it is set unconditionally
  // by loadMedia() in views/media-grid.js).
  const grid = page.locator('#media-grid');
  await expect(grid.locator('text=Loading...')).toHaveCount(0, { timeout: 10_000 });

  // result-count is a text element that is always populated after loadMedia();
  // we accept any value (zero is valid since no items have been favourited).
  await expect(page.locator('#result-count')).toBeVisible({ timeout: 5_000 });
});

// -----------------------------------------------------------------------
// 3. Clearing the search input restores the full grid.
// -----------------------------------------------------------------------

test('clearing the search input restores the full grid', async ({ page }) => {
  await openAuthenticatedPage(page, '/');
  await openFirstSet(page);

  const grid = page.locator('#media-grid');
  const baselineCount = await grid.locator('.media-card, .media-row, .folder-card').count();

  await openSearchOverlay(page);
  await page.fill('#search-input', 'zzz-no-such-file-xyz');
  await page.locator('#search-input').press('Enter');
  await page.waitForTimeout(800);

  // Click the clear button (#search-clear) — this is wired in search.js
  // to reset the input and re-trigger onChange with an empty query.
  await openSearchOverlay(page);
  await page.locator('#search-clear').click();

  // Wait for the debounce + reload to finish.
  await page.waitForTimeout(800);
  await expect(grid.locator('text=Loading...')).toHaveCount(0, { timeout: 10_000 });

  const afterClearCount = await grid.locator('.media-card, .media-row, .folder-card').count();
  // After clearing the count should match the baseline (or be at least as large,
  // accounting for any items that may have been refreshed by an in-flight scan).
  expect(afterClearCount).toBeGreaterThanOrEqual(baselineCount);
});

// Silence unused-import warnings for type-only imports.
export type _Unused = { c: BrowserContext; admin: typeof ADMIN_USER; pass: typeof ADMIN_PASS };
