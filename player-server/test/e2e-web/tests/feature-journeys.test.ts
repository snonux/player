import { test, expect } from '@playwright/test';
import { bootstrap, triggerRescan, waitForScanIdle, waitForServer } from './helpers/server';

test.use({ serviceWorkers: 'block' });

const baseURL = process.env.PLAYER_URL || 'http://localhost:8080';
let adminCookie: string;

test.beforeAll(async () => {
  test.setTimeout(100_000);
  await waitForServer();
  adminCookie = await bootstrap();
  await triggerRescan(adminCookie);
  await waitForScanIdle(adminCookie);
});

test.beforeEach(async ({ page }) => {
  const origin = new URL(baseURL);
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.goto('/');
});

test('tag load failure is shown to the user and can be retried', async ({ page }) => {
  await page.locator('.set-card').filter({ hasText: 'images' }).locator('.title').click();
  const card = page.locator('#media-grid .image-card').first();
  await expect(card).toBeVisible();
  await page.route(/\/api\/media\/\d+$/, route => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'tag detail failed' }) }));
  await card.locator('[data-action="tags"]').click();
  await expect(page.locator('#toast')).toContainText('tag detail failed');
  await page.unrouteAll();
  await card.locator('[data-action="tags"]').click();
  await expect(page.locator('#tags-modal')).toHaveClass(/open/);
});

test('tags can be added, filtered, and removed through the browser', async ({ page }) => {
  await page.getByRole('button', { name: 'Set images' }).click();
  const card = page.locator('#media-grid .image-card').first();
  await expect(card).toBeVisible();
  const id = await card.getAttribute('data-id');
  const tag = `ci2-browser-${Date.now()}`;

  await card.locator('[data-action="tags"]').click();
  await expect(page.locator('#tags-modal')).toHaveClass(/open/);
  await page.locator('#tags-new').fill(tag);
  await page.locator('#tags-add').click();
  await expect(page.locator('#tags-list .tag-chip').filter({ hasText: tag })).toHaveCount(1);
  await page.locator('#tags-close').click();

  await page.keyboard.press('/');
  await expect(page.locator('#search-input')).toBeFocused();
  await page.locator('#search-input').fill(`tag:${tag}`);
  await expect(page.locator(`#media-grid .image-card[data-id="${id}"]`)).toBeVisible();
  await expect(page.locator('#media-grid .image-card')).toHaveCount(1);
  await page.locator(`#media-grid .image-card[data-id="${id}"] [data-action="tags"]`).click();
  await page.locator('#tags-list .tag-chip').filter({ hasText: tag }).locator('.tag-remove').click();
  await expect(page.locator('#tags-list .tag-chip').filter({ hasText: tag })).toHaveCount(0);
  await page.locator('#tags-close').click();
  await expect(page.locator('#media-grid')).toContainText('No results');
});

test('closing Tags with Escape restores vi navigation and search', async ({ page }) => {
  await page.getByRole('button', { name: 'Set images' }).click();
  const card = page.locator('#media-grid .image-card').first();
  await expect(card).toBeVisible();
  await card.locator('[data-action="tags"]').click();
  await expect(page.locator('#tags-modal')).toHaveClass(/open/);
  await expect(page.locator('#tags-new')).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(page.locator('#tags-modal')).not.toHaveClass(/open/);
  await expect(card).toBeFocused();
  // The fixture images fit in one grid row, so vi 'l' moves to the next card.
  await page.keyboard.press('l');
  await expect(page.locator('#media-grid .media-card.selected')).not.toHaveAttribute('data-id', (await card.getAttribute('data-id')) || '');
  await expect(page.locator('#media-grid .media-card.selected')).toHaveCount(1);
  await page.keyboard.press('/');
  await expect(page.locator('#search-input')).toBeFocused();
});

test('folder browsing and Backspace return to the set and set grid', async ({ page }) => {
  let coverPosts = 0;
  page.on('request', request => {
    if (request.method() === 'POST' && /\/api\/sets\/\d+\/cover/.test(request.url())) coverPosts++;
  });
  await page.getByRole('button', { name: 'Set audiobooks' }).click();
  const folder = page.getByRole('button', { name: 'Folder aesops-fables', exact: true });
  await expect(folder).toBeVisible();
  await folder.click();
  await expect(page.locator('#breadcrumb-bar')).toContainText('aesops-fables');
  expect(coverPosts).toBe(0);
  await expect(page.locator('#media-grid .audio-card').first()).toBeVisible();
  await page.keyboard.press('Backspace');
  await expect(folder).toBeVisible();
  await folder.focus();
  await expect(page.locator('#media-grid .folder-card').filter({ has: folder })).toHaveClass(/selected/);
  await page.keyboard.press('Enter');
  await expect(page.locator('#breadcrumb-bar')).toContainText('aesops-fables');
  // Inside the folder, vi h/l move the selection between episode cards.
  const cards = page.locator('#media-grid .audio-card');
  await cards.first().focus();
  await expect(cards.first()).toHaveClass(/selected/);
  await page.keyboard.press('l');
  await expect(cards.nth(1)).toHaveClass(/selected/);
  await expect(page.locator('#media-grid .selected')).toHaveCount(1);
  await page.keyboard.press('h');
  await expect(cards.first()).toHaveClass(/selected/);
  await page.keyboard.press('Backspace');
  await expect(folder).toBeVisible();
  const coverRequest = page.waitForRequest(request => request.method() === 'POST' && /\/api\/sets\/\d+\/cover/.test(request.url()));
  await page.getByRole('button', { name: 'Regenerate cover for folder aesops-fables' }).click();
  await coverRequest;
  await expect(page.locator('#toast')).toContainText(/Folder cover regenerated|no media files available for cover/);
  await folder.focus();
  await page.keyboard.press('Backspace');
  await expect(page.getByRole('button', { name: 'Set audiobooks' })).toBeVisible();
});

test('open dialogs keep vi and admin shortcuts from acting behind them', async ({ page }) => {
  let rescans = 0;
  page.on('request', request => {
    if (request.method() === 'POST' && request.url().endsWith('/rescan')) rescans++;
  });
  await page.getByRole('button', { name: 'Set images' }).click();
  const card = page.locator('#media-grid .image-card').first();
  await expect(card).toBeVisible();
  const id = await card.getAttribute('data-id');
  await card.locator('[data-action="tags"]').click();
  await expect(page.locator('#tags-modal')).toHaveClass(/open/);
  // A focused dialog button used to swallow every key; now it only keeps
  // native activation, so the dialog guard must stop grid and admin keys.
  await page.locator('#tags-close').focus();
  await page.keyboard.press('l');
  await page.keyboard.press('M');
  // ? must not stack help underneath the open Tags dialog.
  await page.keyboard.press('?');
  await expect(page.locator('#help-modal')).not.toHaveClass(/open/);
  await expect(page.locator('#tags-modal')).toHaveClass(/open/);
  await expect(page.locator('#media-grid .media-card.selected')).toHaveAttribute('data-id', id || '');
  expect(rescans).toBe(0);
  await page.keyboard.press('Escape');
  await expect(page.locator('#tags-modal')).not.toHaveClass(/open/);
  await expect(card).toBeFocused();
});

test('a focused toolbar button keeps / search and Enter activation', async ({ page }) => {
  const help = page.locator('#help-toggle');
  await help.focus();
  await page.keyboard.press('/');
  await expect(page.locator('#search-input')).toBeFocused();
  await page.keyboard.press('Escape');
  await help.focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('#help-modal')).toHaveClass(/open/);
  // With help as the only dialog, ? closes it again.
  await page.keyboard.press('?');
  await expect(page.locator('#help-modal')).not.toHaveClass(/open/);
  await help.focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('#help-modal')).toHaveClass(/open/);
  await page.keyboard.press('Escape');
  await expect(page.locator('#help-modal')).not.toHaveClass(/open/);
});

test('Space toggles a focused set row but not sidebar buttons', async ({ page }) => {
  // The header auto-hides; open the sidebar the keyboard way.
  await page.locator('#sidebar-toggle').focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('#sidebar')).toHaveClass(/open/);
  const row = page.locator('#set-list .set-row').filter({ hasText: 'images' });
  const item = row.locator('.set-item');
  await row.focus();
  const before = await item.evaluate(el => el.classList.contains('selected'));
  await page.keyboard.press(' ');
  await expect.poll(() => item.evaluate(el => el.classList.contains('selected'))).toBe(!before);
  await page.keyboard.press(' ');
  await expect.poll(() => item.evaluate(el => el.classList.contains('selected'))).toBe(before);

  // The row's cover button and the Close button are not set rows: Space must
  // not toggle the set (it is play/pause, a no-op with nothing loaded).
  let coverPosts = 0;
  page.on('request', request => {
    if (request.method() === 'POST' && /\/cover/.test(request.url())) coverPosts++;
  });
  for (const button of [row.locator('[data-cover-set]'), page.locator('#menu-close')]) {
    await button.focus();
    await page.keyboard.press(' ');
    expect(await item.evaluate(el => el.classList.contains('selected'))).toBe(before);
  }
  expect(coverPosts).toBe(0);
  await expect(page.locator('#sidebar')).toHaveClass(/open/);
});

test('a keyboard-focused set row shows the accent focus outline', async ({ page }) => {
  await page.locator('#sidebar-toggle').focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('#sidebar')).toHaveClass(/open/);
  const row = page.locator('#set-list .set-row').filter({ hasText: 'images' });
  // Focus follows a keyboard action (Enter above), so :focus-visible applies.
  await row.focus();
  await expect(row).toBeFocused();
  const style = await row.evaluate(el => {
    const css = (globalThis as any).getComputedStyle(el);
    const probe = (globalThis as any).document.createElement('span');
    probe.style.color = css.getPropertyValue('--accent');
    el.appendChild(probe);
    const accent = (globalThis as any).getComputedStyle(probe).color;
    probe.remove();
    return { outline: css.outlineStyle, color: css.outlineColor, accent };
  });
  expect(style.outline).toBe('solid');
  expect(style.color).toBe(style.accent);
});

test('dialogs take focus, keep Tab inside, and return focus on close', async ({ page }) => {
  // Keyboard-opened admin panel: first form field gets focus.
  const toggle = page.locator('#admin-toggle');
  await toggle.focus();
  await page.keyboard.press('Enter');
  const panel = page.locator('#admin-modal');
  await expect(panel).toHaveClass(/open/);
  await expect(panel.getByPlaceholder('Username')).toBeFocused();

  // Tab and Shift+Tab never leave the open dialog.
  for (let i = 0; i < 25; i++) {
    await page.keyboard.press(i % 3 === 2 ? 'Shift+Tab' : 'Tab');
    expect(await page.evaluate(() => Boolean((globalThis as any).document.activeElement?.closest('#admin-modal')))).toBe(true);
  }

  // Closing with the ✕ button returns focus to the opener.
  await panel.getByRole('button', { name: 'Close' }).click();
  await expect(panel).not.toHaveClass(/open/);
  await expect(toggle).toBeFocused();

  // A shortcut-opened dialog (notes via n) returns focus to the card.
  await page.getByRole('button', { name: 'Set images' }).click();
  const card = page.locator('#media-grid .image-card').first();
  await card.focus();
  await page.keyboard.press('n');
  await expect(page.locator('#notes-modal')).toHaveClass(/open/);
  await expect(page.locator('#notes-textarea')).toBeFocused();
  // One Escape blurs the textarea and closes the unchanged note; the card
  // keeps both focus and its selection.
  await page.keyboard.press('Escape');
  await expect(page.locator('#notes-modal')).not.toHaveClass(/open/);
  await expect(card).toBeFocused();
  await expect(card).toHaveClass(/selected/);
});

test('theme toggle switches by keyboard and survives a reload', async ({ page }) => {
  const html = page.locator('html');
  const toggle = page.locator('#theme-toggle');
  const before = await html.getAttribute('data-theme') || 'dark';
  const after = before === 'dark' ? 'light' : 'dark';
  await toggle.focus();
  await page.keyboard.press('Enter');
  await expect(html).toHaveAttribute('data-theme', after);
  const background = await page.evaluate(() => (globalThis as any).getComputedStyle((globalThis as any).document.body).backgroundColor);
  await page.reload();
  await expect(html).toHaveAttribute('data-theme', after);
  expect(await page.evaluate(() => (globalThis as any).getComputedStyle((globalThis as any).document.body).backgroundColor)).toBe(background);
  // Restore for the other tests.
  await page.locator('#theme-toggle').focus();
  await page.keyboard.press('Enter');
  await expect(html).toHaveAttribute('data-theme', before);
});

test('t, F and A reach tags, favorites and the admin panel from the keyboard', async ({ page }) => {
  await page.getByRole('button', { name: 'Set images' }).click();
  const card = page.locator('#media-grid .image-card').nth(2);
  await card.focus();
  const id = await card.getAttribute('data-id');

  await page.keyboard.press('t');
  await expect(page.locator('#tags-modal')).toHaveClass(/open/);
  await page.keyboard.press('Escape');
  await expect(card).toBeFocused();

  const heart = card.locator('[data-action="favorite"]');
  const wasFavorite = await heart.evaluate(el => el.classList.contains('active'));
  await page.keyboard.press('F');
  await expect(heart).toHaveAttribute('aria-pressed', String(!wasFavorite));
  await expect(page.locator('#toast')).toContainText(wasFavorite ? 'Removed from favorites' : 'Added to favorites');
  const favorites = await page.evaluate(async () => (await fetch('/api/media?favorites=true')).json()) as any[];
  expect(favorites.some((m: any) => String(m.id) === id)).toBe(!wasFavorite);
  await page.keyboard.press('F');
  await expect(heart).toHaveAttribute('aria-pressed', String(wasFavorite));

  await page.keyboard.press('A');
  await expect(page.locator('#admin-modal')).toHaveClass(/open/);
  await page.keyboard.press('Escape');
  await expect(page.locator('#admin-modal')).not.toHaveClass(/open/);
});
