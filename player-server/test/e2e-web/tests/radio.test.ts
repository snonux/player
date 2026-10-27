// Web radio end to end: explorer filters, relayed live playback, bookmarks
// kept by the server, auto explore and the hand-over with library playback.
// Run with playwright.radio.config.ts, which starts a fake Radio Browser
// (tests/helpers/fake-radio-browser.mjs) and a Player server pointed at it.
import { test, expect, type Page } from '@playwright/test';
import { headerItem } from './helpers/header-menu';

const USER = 'radio-admin';
const PASS = 'radio-passw0rd!';

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ request }) => {
  const res = await request.post('/api/bootstrap', { data: { username: USER, password: PASS } });
  // A second browser project finds the account already there.
  expect(res.status()).toBeLessThan(500);
});

async function openRadio(page: Page) {
  const login = await page.request.post('/api/login', { data: { username: USER, password: PASS } });
  expect(login.ok()).toBeTruthy();
  // Each test starts without bookmarks.
  for (const s of await (await page.request.get('/api/radio/bookmarks')).json()) {
    await page.request.delete(`/api/radio/bookmarks/${s.uuid}`);
  }
  await page.goto('/');
  await expect(page.locator('#header-menu-toggle')).toBeVisible();
  await page.keyboard.press('w');
  await expect(page.locator('#radio-modal')).toHaveClass(/open/);
  await expect(page.locator('#radio-results .radio-row')).toHaveCount(5);
}

async function audioProgresses(page: Page) {
  const audio = page.locator('#radio-audio');
  await expect.poll(() => audio.evaluate((el: HTMLAudioElement) => el.currentTime), { timeout: 20_000 }).toBeGreaterThan(1.5);
  expect(await audio.evaluate((el: HTMLAudioElement) => el.paused)).toBe(false);
}

test('explore, filter, play and bookmark a station with the keyboard', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await openRadio(page);
  // HLS stations are filtered out by the server.
  await expect(page.locator('#radio-results')).not.toContainText('Fake HLS Jazz');
  await expect(page.locator('#radio-tag option')).toContainText(['Any genre', 'jazz (3)', 'rock (1)', 'news (1)']);
  await expect(page.locator('#radio-country option[value="FR"]')).toHaveText('France (2)');

  // Name search.
  await page.locator('#radio-q').fill('rock');
  await page.locator('#radio-q').press('Enter');
  await expect(page.locator('#radio-results .radio-row')).toHaveCount(1);
  await expect(page.locator('#radio-results')).toContainText('Fake Rock Radio');
  await expect(page.locator('#radio-results')).toContainText('France · MP3 64 kbit/s · rock');

  // Genre and country filters.
  await page.locator('#radio-q').fill('');
  await page.locator('#radio-tag').selectOption('jazz');
  await expect(page.locator('#radio-results .radio-row')).toHaveCount(3);
  await page.locator('#radio-country').selectOption('DE');
  await expect(page.locator('#radio-results .radio-row')).toHaveCount(2);
  await page.locator('#radio-order').selectOption('name');
  await expect(page.locator('#radio-results .radio-row-name')).toHaveText(['Fake Jazz One', 'Fake Jazz Two']);

  // j/k move between stations, Enter plays.
  await page.locator('#radio-search').focus();
  await page.keyboard.press('j');
  await expect(page.locator('#radio-results .radio-play').first()).toBeFocused();
  await page.keyboard.press('j');
  await expect(page.locator('#radio-results .radio-play').nth(1)).toBeFocused();
  await page.keyboard.press('k');
  await page.keyboard.press('Enter');
  await expect(page.locator('#radio-now-name')).toHaveText('Fake Jazz One');
  await expect(page.locator('#radio-audio')).toHaveAttribute('src', /\/api\/radio\/stations\/00000000-0000-4000-8000-000000000001\/stream$/);
  await audioProgresses(page);
  await expect(page.locator('#radio-status')).toHaveText('Playing');
  await expect(page.locator('#radio-results .radio-row.playing')).toContainText('Fake Jazz One');
  await expect(page.locator('#radio-now-icon img')).toHaveAttribute('src', /\/favicon$/);
  await expect(page.locator('#radio-now-homepage')).toHaveAttribute('href', 'https://example.org/1');

  // Bookmark from the list; the bookmark is stored on the server.
  const star = page.locator('#radio-results .radio-row').first().locator('.radio-bookmark');
  await star.click();
  await expect(star).toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('#radio-now-bookmark')).toHaveAttribute('aria-pressed', 'true');
  const saved = await (await page.request.get('/api/radio/bookmarks')).json();
  expect(saved.map((s: { name: string }) => s.name)).toEqual(['Fake Jazz One']);

  await expect(page.locator('#radio-bar')).toBeHidden();
  await page.screenshot({ path: process.env.RADIO_SCREENSHOT || 'test-results/web-radio.png' });

  // Escape closes the dialog; the station keeps playing in the radio bar.
  await page.keyboard.press('Escape');
  await expect(page.locator('#radio-modal')).not.toHaveClass(/open/);
  await expect(page.locator('#radio-bar')).toBeVisible();
  await expect(page.locator('#radio-bar-title')).toHaveText('Fake Jazz One');
  await audioProgresses(page);

  // After a reload the bookmark is still there; remove it from that tab.
  await page.reload();
  await (await headerItem(page, '#radio-toggle')).click();
  await page.locator('#radio-tab-bookmarks').click();
  await expect(page.locator('#radio-bookmarks .radio-row')).toHaveCount(1);
  await expect(page.locator('#radio-bookmarks')).toContainText('Fake Jazz One');
  await page.locator('#radio-bookmarks .radio-play').click();
  await audioProgresses(page);
  await page.locator('#radio-bookmarks .radio-bookmark').click();
  await expect(page.locator('#radio-bookmarks .radio-row')).toHaveCount(0);
  await expect(page.locator('#radio-bookmarks-status')).toContainText('No bookmarked stations yet');
  expect(await (await page.request.get('/api/radio/bookmarks')).json()).toEqual([]);

  // Stop ends the stream.
  await page.locator('#radio-toggle-play').click();
  await expect(page.locator('#radio-audio')).not.toHaveAttribute('src', /.+/);
  await expect(page.locator('#radio-toggle-play')).toHaveText('Play');
  expect(errors).toEqual([]);
});

test('auto explore moves through the stations of a genre and skips dead ones', async ({ page }) => {
  await openRadio(page);
  // A filter whose only station is dead ends the run with a message.
  await page.locator('#radio-q').fill('dead');
  await page.locator('#radio-q').press('Enter');
  await expect(page.locator('#radio-results .radio-row')).toHaveCount(1);
  await page.locator('#radio-explore').click();
  await expect(page.locator('#radio-status')).toHaveText('No station of these filters could be played', { timeout: 20_000 });
  await expect(page.locator('#radio-explore-controls')).toBeHidden();

  // Jazz: two live stations and the dead one.
  await page.locator('#radio-q').fill('');
  await page.locator('#radio-tag').selectOption('jazz');
  await expect(page.locator('#radio-results .radio-row')).toHaveCount(3);
  await page.locator('#radio-dwell').selectOption('15');
  await page.locator('#radio-explore').click();
  await expect(page.locator('#radio-explore-controls')).toBeVisible();
  const name = page.locator('#radio-now-name');
  const live = /^Fake Jazz (One|Two)$/;
  await expect(name).toHaveText(live, { timeout: 20_000 });
  await audioProgresses(page);
  await expect(page.locator('#radio-countdown')).toHaveText(/^Next station in \d+ s$/);
  const first = await name.textContent();

  // Next skips at once (past the dead station if it comes up).
  await page.locator('#radio-explore-next').click();
  await expect(name).toHaveText(live, { timeout: 20_000 });
  await expect(name).not.toHaveText(first!, { timeout: 20_000 });
  await audioProgresses(page);
  const second = await name.textContent();

  // The dwell time runs out and the explorer moves on by itself.
  await expect(name).not.toHaveText(second!, { timeout: 30_000 });
  await expect(name).toHaveText(live, { timeout: 20_000 });

  // Bookmark while exploring, then keep the station.
  await page.locator('#radio-now-bookmark').click();
  await expect(page.locator('#radio-now-bookmark')).toHaveAttribute('aria-pressed', 'true');
  await page.locator('#radio-explore-keep').click();
  await expect(page.locator('#radio-explore-controls')).toBeHidden();
  const kept = await name.textContent();
  await page.waitForTimeout(16_000);
  await expect(name).toHaveText(kept!);
  await audioProgresses(page);
  const saved = await (await page.request.get('/api/radio/bookmarks')).json();
  expect(saved.map((s: { name: string }) => s.name)).toEqual([kept]);
});

test('radio and library playback stop each other', async ({ page }) => {
  await openRadio(page);
  await page.locator('#radio-results .radio-play').first().click();
  await audioProgresses(page);
  // Library playback (here the library audio element playing another
  // stream) stops the radio.
  await page.evaluate(async () => {
    const el = document.getElementById('media-audio') as HTMLAudioElement;
    el.src = '/api/radio/stations/00000000-0000-4000-8000-000000000002/stream';
    await el.play();
  });
  await expect(page.locator('#radio-audio')).not.toHaveAttribute('src', /.+/);
  await expect(page.locator('#radio-status')).toHaveText('Stopped');
  // Starting the radio pauses the library.
  await page.locator('#radio-results .radio-play').first().click();
  await audioProgresses(page);
  expect(await page.locator('#media-audio').evaluate((el: HTMLAudioElement) => el.paused)).toBe(true);
});
