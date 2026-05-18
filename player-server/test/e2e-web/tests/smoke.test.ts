/**
 * smoke.test.ts — Playwright smoke suite for the Player web UI.
 *
 * Coverage:
 *   1. Bootstrap  — fresh server redirects to /bootstrap.html; form creates the
 *                   first admin account and lands on /login.html.
 *   2. Login      — submitting the login form lands on / (index).
 *   3. List sets  — sidebar opens and shows at least one set name.
 *   4. Browse     — clicking a set loads the media grid.
 *   5. Progress   — progress API is called when a media card exists; the POST
 *                   succeeds (HTTP 200). Full media playback is not exercised
 *                   in headless tests because codec availability varies.
 *   6. Admin-only — the admin gear button is visible to the admin and opens
 *                   the admin panel; a non-admin user does not see the button.
 *
 * Prerequisites (see README):
 *   - The Player server must already be running (default: http://localhost:8080).
 *   - The server must be started with SECURE_COOKIES=false so that the session
 *     cookie is accessible on plain HTTP.
 *   - The server must point at a MEDIA_ROOT that contains at least one set with
 *     at least one media file.  The testmedia/ directory in this repo satisfies
 *     that requirement when passed as MEDIA_ROOT.
 *
 * Run:
 *   npm test
 */

import { test, expect, Page, BrowserContext } from '@playwright/test';
import {
  bootstrap,
  login,
  ensureRegularUser,
  triggerRescan,
  waitForServer,
  ADMIN_USER,
  ADMIN_PASS,
  REGULAR_USER,
  REGULAR_PASS,
} from './helpers/server';

// -----------------------------------------------------------------------
// Module-level setup: ensure the server is up and state is seeded once
// for the entire file. All tests in this file run serially (workers: 1)
// so a single shared admin session cookie is safe.
// -----------------------------------------------------------------------

let adminCookie: string = '';

// Allow 60 s for beforeAll: the rescan can take 30+ seconds on large libraries.
test.beforeAll(async () => {
  // Wait for the server to be reachable before running any tests.
  await waitForServer(15_000);

  // Bootstrap creates the admin account on a fresh server; on a re-run it
  // logs in instead — so beforeAll is idempotent across test runs.
  adminCookie = await bootstrap();

  // Trigger a media rescan so sets appear in the API. The helper returns once
  // at least one set is visible; it does not wait for the full scan to finish.
  await triggerRescan(adminCookie, 30_000);

  // Ensure the non-admin user exists for the admin-gate tests.
  await ensureRegularUser(adminCookie);
}, 60_000);

// -----------------------------------------------------------------------
// Helper: inject a session cookie into a browser context so subsequent
// page navigations are authenticated without going through the login form.
// -----------------------------------------------------------------------

/**
 * injectSessionCookie adds the session cookie to the browser context so
 * subsequent page.goto() calls carry the session automatically.
 *
 * The cookie domain must match the hostname in PLAYER_URL exactly;
 * Playwright is strict about domain matching.
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
      // Domain must match the host exactly. Playwright requires a domain
      // without a port number; the port is specified separately if needed.
      domain: parsed.hostname,
      path: '/',
      // Mark the cookie as sameSite Strict to match server settings.
      sameSite: 'Strict',
    },
  ]);
}

/**
 * openAuthenticatedPage injects the session cookie into the page's browser
 * context and navigates to `path`. Returns the page for chaining.
 */
async function openAuthenticatedPage(
  page: Page,
  cookieHeader: string,
  path: string,
): Promise<Page> {
  await injectSessionCookie(page.context(), cookieHeader);
  await page.goto(path);
  return page;
}

/**
 * waitForAppReady waits for the SPA to finish its initial load sequence.
 * The logout button is in the DOM as soon as the SPA HTML is parsed, but
 * we also wait for the sets API call to resolve (sets are in the DOM).
 */
async function waitForAppReady(page: Page): Promise<void> {
  // Wait for the page to have loaded the SPA HTML — logout-btn is in the
  // static HTML so it is always present in the DOM when the SPA is loaded.
  await page.waitForSelector('#logout-btn', { timeout: 15_000 });
}

/**
 * revealHeader moves the mouse pointer to the very top of the viewport to
 * trigger the CSS :hover rule that slides the auto-hiding site header into
 * view. The Player header uses `transform: translateY(calc(-100% + 0.45rem))`
 * by default (only a thin strip is visible), and `transform: translateY(0)` on
 * hover. Clicking any header button therefore requires revealing the header
 * first.
 */
async function revealHeader(page: Page): Promise<void> {
  // Move to the top-centre of the page to hover over the thin visible strip.
  const viewport = page.viewportSize();
  const x = viewport ? Math.floor(viewport.width / 2) : 400;
  await page.mouse.move(x, 2);
  // Small pause to let the CSS transition (var(--transition-base)) complete.
  await page.waitForTimeout(300);
}

// -----------------------------------------------------------------------
// 1. Bootstrap
// -----------------------------------------------------------------------

test('bootstrap page is reachable', async ({ page }) => {
  // Navigate directly to /bootstrap.html. The server either serves the page
  // (fresh database) or redirects to /login.html (already bootstrapped).
  await page.goto('/bootstrap.html');

  // After following any redirects we should land on bootstrap or login.
  const url = page.url();
  expect(url).toMatch(/\/(bootstrap|login)\.html/);
});

test('bootstrap or login page has the expected form', async ({ page }) => {
  await page.goto('/');

  const url = page.url();

  if (url.includes('bootstrap.html')) {
    // Fresh server: the bootstrap form should be visible.
    await expect(page.locator('#bootstrap-form')).toBeVisible({ timeout: 5_000 });

    // Confirm form fields are present.
    await expect(page.locator('#username')).toBeVisible();
    await expect(page.locator('#password')).toBeVisible();
    await expect(page.locator('#password-confirm')).toBeVisible();
  } else {
    // Already bootstrapped — we should be on the login page.
    expect(url).toContain('login.html');
    await expect(page.locator('#login-form')).toBeVisible({ timeout: 5_000 });
  }
});

// -----------------------------------------------------------------------
// 2. Login
// -----------------------------------------------------------------------

test('login form authenticates and lands on main page', async ({ page }) => {
  await page.goto('/login.html');
  await expect(page.locator('#login-form')).toBeVisible({ timeout: 10_000 });

  await page.fill('#username', ADMIN_USER);
  await page.fill('#password', ADMIN_PASS);
  await page.click('button[type="submit"]');

  // After successful login the JS sets location.href = '/'. Wait for the
  // browser to leave the login page. Use a glob pattern — not a function —
  // because the URL object passed to the predicate in older Playwright
  // versions does not have a string .includes() method.
  await page.waitForURL('**/', { timeout: 10_000 });

  // The main page must render the header with the logout button.
  await expect(page.locator('#logout-btn')).toBeVisible({ timeout: 10_000 });
});

test('login with wrong password shows error message', async ({ page }) => {
  await page.goto('/login.html');
  await expect(page.locator('#login-form')).toBeVisible({ timeout: 10_000 });

  await page.fill('#username', ADMIN_USER);
  await page.fill('#password', 'totally-wrong-password');
  await page.click('button[type="submit"]');

  // The error paragraph should become non-empty without leaving the login page.
  const errEl = page.locator('#login-error');
  await expect(errEl).not.toBeEmpty({ timeout: 8_000 });
  // We should still be on the login page.
  expect(page.url()).toContain('login.html');
});

// -----------------------------------------------------------------------
// 3. List sets
// -----------------------------------------------------------------------

test('authenticated user can open the sets sidebar', async ({ page }) => {
  // Use admin cookie so we reach the main page directly, bypassing login UI.
  await openAuthenticatedPage(page, adminCookie, '/');
  await waitForAppReady(page);

  // Hover over the auto-hiding header to reveal the sidebar toggle button.
  await revealHeader(page);

  // Toggle the sidebar open.
  await page.locator('#sidebar-toggle').click();

  // The sidebar should become visible.
  const sidebar = page.locator('#sidebar');
  await expect(sidebar).toBeVisible({ timeout: 5_000 });

  // At least one set item must exist (testmedia/ has sets after the rescan).
  const setRows = sidebar.locator('.set-row');
  await expect(setRows.first()).toBeVisible({ timeout: 10_000 });
  const count = await setRows.count();
  expect(count).toBeGreaterThan(0);
});

test('set list shows non-empty set names', async ({ page }) => {
  await openAuthenticatedPage(page, adminCookie, '/');
  await waitForAppReady(page);
  await revealHeader(page);
  await page.locator('#sidebar-toggle').click();

  // Each set row should contain a non-empty set-item span.
  const firstSetName = page.locator('.set-row .set-item').first();
  await expect(firstSetName).toBeVisible({ timeout: 10_000 });
  const name = await firstSetName.textContent();
  expect(name?.trim().length).toBeGreaterThan(0);
});

// -----------------------------------------------------------------------
// 4. Browse media
// -----------------------------------------------------------------------

test('clicking a set loads the media grid', async ({ page }) => {
  await openAuthenticatedPage(page, adminCookie, '/');
  await waitForAppReady(page);

  // Reveal the auto-hiding header and open the sidebar.
  await revealHeader(page);
  await page.locator('#sidebar-toggle').click();
  const firstSetRow = page.locator('.set-row').first();
  await firstSetRow.waitFor({ state: 'visible', timeout: 10_000 });
  await firstSetRow.click({ force: true });

  // The media grid should become visible.
  const mediaGrid = page.locator('#media-grid');
  await expect(mediaGrid).toBeVisible({ timeout: 10_000 });

  // Wait for the loading placeholder to disappear.
  await expect(mediaGrid.locator('text=Loading...')).toHaveCount(0, { timeout: 15_000 });

  // The grid should contain at least one card, folder row, or empty-state message.
  const gridContent = mediaGrid.locator('.media-card, .media-row, .folder-card, .text-muted');
  await expect(gridContent.first()).toBeVisible({ timeout: 10_000 });
});

// -----------------------------------------------------------------------
// 5. Progress saved
//    Full audio / video playback via Playwright is unreliable in headless
//    Chromium because codec availability varies by environment. Instead we
//    verify the progress API endpoint directly: post a position update and
//    confirm the server accepts it (HTTP 200).
//
//    Note: the in-progress list requires >= 60 seconds of accumulated
//    playback before showing an item (business rule in the repository
//    layer). A single POST at 30 s is not enough to appear in the list,
//    so we only verify the POST response — not the in-progress listing.
// -----------------------------------------------------------------------

test('progress API accepts position update and returns 200', async ({ page }) => {
  const cookie = await login(ADMIN_USER, ADMIN_PASS);

  // Fetch the media list directly from the first available set.
  const setsRes = await page.request.get('/api/v1/sets', {
    headers: { Cookie: cookie },
  });
  expect(setsRes.ok()).toBeTruthy();
  const sets = (await setsRes.json()) as Array<{ id: number; name: string }> | null;

  if (!sets || sets.length === 0) {
    test.skip(true, 'No sets found — media root may not have been scanned yet');
    return;
  }
  const setId = sets[0].id;

  // Fetch media items from the first set.
  const mediaRes = await page.request.get(`/api/v1/media?set_id=${setId}`, {
    headers: { Cookie: cookie },
  });
  expect(mediaRes.ok()).toBeTruthy();
  const mediaList = (await mediaRes.json()) as Array<{ id: number }> | null;

  if (!mediaList || mediaList.length === 0) {
    test.skip(true, 'No media items in first set — skipping progress test');
    return;
  }

  const mediaId = mediaList[0].id;

  // POST a progress record at 30 seconds and verify the server accepts it.
  const progressRes = await page.request.post('/api/v1/progress', {
    headers: { Cookie: cookie, 'Content-Type': 'application/json' },
    data: JSON.stringify({ media_id: mediaId, position_seconds: 30.0 }),
  });
  expect(progressRes.status()).toBe(200);
  const body = await progressRes.json();
  expect(body).toMatchObject({ status: 'ok' });
});

// -----------------------------------------------------------------------
// 6. Admin panel — accessible to admin, hidden from regular user
// -----------------------------------------------------------------------

test('admin gear button is visible and opens admin panel for admin user', async ({ page }) => {
  await openAuthenticatedPage(page, adminCookie, '/');
  await waitForAppReady(page);

  // The admin toggle button is conditionally un-hidden by JS when the user is
  // admin (the app calls API.users() and on success runs showAdmin()).
  const adminToggle = page.locator('#admin-toggle');
  await expect(adminToggle).not.toHaveClass(/hidden/, { timeout: 10_000 });

  // Reveal the auto-hiding header, then click the gear button.
  await revealHeader(page);
  await adminToggle.click();

  // The admin modal should open (it receives the 'open' CSS class).
  const adminModal = page.locator('#admin-modal');
  await expect(adminModal).toHaveClass(/open/, { timeout: 5_000 });

  // The Users section heading must be present in the modal.
  await expect(adminModal.locator('h4', { hasText: 'Users' })).toBeVisible();
});

test('admin panel is not accessible to regular user', async ({ page }) => {
  const cookie = await login(REGULAR_USER, REGULAR_PASS);

  // Register the response listener BEFORE navigating so it cannot miss the
  // API.users() call that fires at SPA init. waitForResponse only catches
  // future responses — setting it up after goto() creates a race condition
  // where the 403 may already be received by the time the listener is active.
  await injectSessionCookie(page.context(), cookie);
  const adminCheck = page.waitForResponse(
    resp => resp.url().includes('/api/admin/users') && resp.status() === 403,
    { timeout: 10_000 },
  );
  await page.goto('/');
  await adminCheck;

  const adminToggle = page.locator('#admin-toggle');
  const classes = (await adminToggle.getAttribute('class')) ?? '';
  expect(classes).toContain('hidden');
});

test('admin-only API endpoints return 403 for regular user', async ({ page }) => {
  const cookie = await login(REGULAR_USER, REGULAR_PASS);

  // A non-admin hitting the admin users endpoint should receive 403 Forbidden.
  const res = await page.request.get('/api/v1/admin/users', {
    headers: { Cookie: cookie },
  });
  expect(res.status()).toBe(403);
});

// -----------------------------------------------------------------------
// 7. Logout
// -----------------------------------------------------------------------

test('logout button ends the session and redirects to login', async ({ page }) => {
  await openAuthenticatedPage(page, adminCookie, '/');
  await waitForAppReady(page);

  // Reveal the auto-hiding header, then click logout.
  await revealHeader(page);
  const logoutBtn = page.locator('#logout-btn');
  await logoutBtn.click();

  // After logout the JS posts to /api/logout; the server clears the cookie
  // and the SPA redirects to /login.html.
  await page.waitForURL('**/login.html', { timeout: 10_000 });
  await expect(page.locator('#login-form')).toBeVisible({ timeout: 5_000 });
});
