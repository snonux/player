import { test, expect, type Page } from '@playwright/test';
import { bootstrap, triggerRescan, waitForScanIdle, waitForServer } from './helpers/server';

test.use({ serviceWorkers: 'block' });
const baseURL = process.env.PLAYER_URL || 'http://localhost:8080';
type Media = { id: number; type: string; file_name: string; duration: number };
let cookie: string;
let media: Media[];
const headers = () => ({ Cookie: cookie });

test.beforeAll(async () => {
  test.setTimeout(90_000);
  await waitForServer();
  cookie = await bootstrap();
  await triggerRescan(cookie);
  await waitForScanIdle(cookie);
  media = await (await fetch(`${baseURL}/api/media`, { headers: headers() })).json() as Media[];
  expect(media.some(item => item.type === 'audio')).toBeTruthy();
  expect(media.some(item => item.type === 'video')).toBeTruthy();
});

async function load(page: Page, item: Media, position = 0) {
  await page.evaluate(async ({ item, position }) => {
    const path = '/js/playback.js';
    const player = await import(path);
    player.loadMediaDirect(item, `/api/media/${item.id}/stream`, '', position);
  }, { item, position });
  if (item.type !== 'image') {
    await expect.poll(() => page.locator(`#media-${item.type}`).evaluate((el: HTMLMediaElement) => el.readyState)).toBeGreaterThanOrEqual(1);
    await page.locator(`#media-${item.type}`).evaluate((el: HTMLMediaElement) => el.pause());
  }
}

async function open(page: Page, type = 'audio') {
  await page.context().addCookies([{ name: 'session', value: cookie.slice(8), url: baseURL }]);
  await page.goto('/');
  await expect(page.locator('#set-list .set-row').first()).toBeAttached();
  const item = media.find(item => item.type === type)!;
  await load(page, item, 3);
  const rows = await (await page.request.get(`/api/media/${item.id}/bookmarks`)).json();
  for (const row of rows) await page.request.delete(`/api/media/${item.id}/bookmarks/${row.id}`);
  return item;
}

async function control(page: Page, id: string) {
  const button = page.locator(`#${id}`);
  if (!await button.isVisible()) {
    await page.getByRole('button', { name: 'More player controls', exact: true }).click();
  }
  return button;
}

for (const type of ['audio', 'video']) {
  test(`${type}: save, reload, jump and delete a personal position without changing notes or resume`, async ({ page }) => {
    const item = await open(page, type);
    await page.request.post(`/api/media/${item.id}/notes`, { data: { content: 'Keep this note' } });
    await page.request.post('/api/progress', { data: { media_id: item.id, position_seconds: 2 } });
    const errors: string[] = [];
    page.on('pageerror', error => errors.push(error.message));
    await (await control(page, 'btn-save-position')).focus();
    await page.keyboard.press('Enter');
    await expect(page.locator('#bookmarks-status')).toHaveText('No saved positions yet.');
    await expect(page.locator('#bookmark-label')).toBeFocused();
    await page.locator('#bookmark-label').fill('<img src=x onerror=alert(1)> chapter');
    await page.locator('#bookmark-label').press('Enter');
    await expect(page.locator('#bookmarks-status')).toHaveText('Position saved.');
    await expect(page.locator('.bookmark-jump')).toContainText('0:03 — <img');
    await expect(page.locator('#bookmarks-list img')).toHaveCount(0);
    await page.locator('#bookmarks-close').click();
    await expect(page.locator('#btn-player-options:visible, #btn-save-position:visible')).toBeFocused();
    await page.reload();
    await expect(page.locator('#set-list .set-row').first()).toBeAttached();
    await load(page, item);
    await (await control(page, 'btn-bookmarks')).click();
    await expect(page.locator('.bookmark-jump')).toContainText('chapter');
    await page.locator('.bookmark-jump').focus();
    await page.keyboard.press('Enter');
    await expect(page.locator('#bookmarks-modal')).toBeHidden();
    await expect.poll(() => page.locator(`#media-${type}`).evaluate((el: HTMLMediaElement) => el.currentTime)).toBeCloseTo(3, 0);
    await (await control(page, 'btn-bookmarks')).click();
    await page.locator('#bookmarks-list button', { hasText: 'Delete' }).click();
    await expect(page.locator('#bookmarks-status')).toHaveText('No saved positions yet.');
    await expect(page.locator('#bookmarks-list li')).toHaveCount(0);
    const detail = await (await page.request.get(`/api/media/${item.id}`)).json();
    expect(detail.progress.position_seconds).toBe(2);
    const note = await (await page.request.get(`/api/media/${item.id}/notes`)).json();
    expect(note.content).toBe('Keep this note');
    expect(errors).toEqual([]);
  });
}

test('typing and dialog buttons keep playback still; Tab cycles and Escape restores opener', async ({ page }) => {
  await open(page);
  await (await control(page, 'btn-save-position')).click();
  await expect(page.locator('#bookmark-save')).toBeEnabled();
  await page.locator('#bookmark-label').pressSequentially('jklNPhl / space');
  await page.locator('#bookmark-label').press('ArrowLeft');
  await expect.poll(() => page.locator('#media-audio').evaluate((el: HTMLMediaElement) => el.currentTime)).toBeCloseTo(3, 0);
  await page.locator('#bookmarks-close').focus();
  await page.keyboard.press('Shift+Tab');
  await expect(page.locator('#bookmark-capture')).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(page.locator('#bookmarks-close')).toBeFocused();
  await page.keyboard.press('l');
  await expect.poll(() => page.locator('#media-audio').evaluate((el: HTMLMediaElement) => el.currentTime)).toBeCloseTo(3, 0);
  await page.locator('#bookmark-label').focus();
  await page.keyboard.press('Escape');
  await expect(page.locator('#bookmarks-modal')).toBeHidden();
  await expect(page.locator('#btn-player-options:visible, #btn-save-position:visible')).toBeFocused();
});

test('load/save/delete failures are visible and retry preserves the label and rows', async ({ page }) => {
  const item = await open(page);
  const path = `/api/media/${item.id}/bookmarks`;
  let failing = 'GET';
  await page.route(/\/api\/media\/\d+\/bookmarks(?:\/\d+)?$/, async route => {
    if (route.request().method() === failing) {
      await route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"test failure"}' });
    } else await route.continue();
  });
  await (await control(page, 'btn-save-position')).click();
  await expect(page.locator('#bookmarks-status')).toHaveText('Could not load bookmarks. Try again.');
  await expect(page.locator('#bookmark-save')).toBeDisabled();
  failing = 'POST';
  await page.locator('#bookmarks-retry').click();
  await expect(page.locator('#bookmark-save')).toBeEnabled();
  await page.locator('#bookmark-label').fill('Draft');
  await page.locator('#bookmark-save').click();
  await expect(page.locator('#bookmarks-status')).toHaveText('Could not save position. Try again.');
  await expect(page.locator('#bookmark-label')).toHaveValue('Draft');
  failing = 'DELETE';
  await page.locator('#bookmark-save').click();
  await expect(page.locator('.bookmark-jump')).toContainText('Draft');
  await page.locator('#bookmarks-list button', { hasText: 'Delete' }).click();
  await expect(page.locator('#bookmarks-status')).toHaveText('Could not delete bookmark. Try again.');
  await expect(page.locator('.bookmark-jump')).toHaveCount(1);
  failing = '';
  await page.locator('#bookmarks-list button', { hasText: 'Delete' }).click();
  await expect(page.locator('#bookmarks-status')).toHaveText('No saved positions yet.');
  expect((await (await page.request.get(path)).json()).length).toBe(0);
});

for (const method of ['GET', 'POST', 'DELETE']) {
  test(`late ${method} response cannot alter another track or its dialog`, async ({ page }) => {
    const first = await open(page);
    const second = media.find(item => item.type === 'video')!;
    if (method === 'DELETE') await page.request.post(`/api/media/${first.id}/bookmarks`, { data: { position_seconds: 1, label: 'Old bookmark' } });
    let release!: () => void;
    const gate = new Promise<void>(resolve => { release = resolve; });
    let started!: () => void;
    const requestStarted = new Promise<void>(resolve => { started = resolve; });
    await page.route(new RegExp(`/api/media/${first.id}/bookmarks(?:/\\d+)?$`), async route => {
      if (route.request().method() === method) { started(); await gate; }
      await route.continue();
    });
    await (await control(page, 'btn-save-position')).click();
    if (method === 'POST') {
      await expect(page.locator('#bookmark-save')).toBeEnabled();
      await page.locator('#bookmark-label').fill('Old track');
      await page.locator('#bookmark-save').click();
    } else if (method === 'DELETE') {
      await page.locator('#bookmarks-list button', { hasText: 'Delete' }).click();
    }
    await requestStarted;
    await load(page, second, 4);
    await expect(page.locator('#bookmarks-modal')).toBeHidden();
    await (await control(page, 'btn-save-position')).click();
    await expect(page.locator('#bookmark-save')).toBeEnabled();
    await page.locator('#bookmark-label').fill('New track draft');
    release();
    await page.waitForLoadState('networkidle');
    await expect(page.locator('#bookmarks-media')).toHaveText(second.file_name);
    await expect(page.locator('#bookmark-label')).toHaveValue('New track draft');
    await expect(page.locator('.bookmark-jump', { hasText: /Old/ })).toHaveCount(0);
    await expect.poll(() => page.locator('#media-video').evaluate((el: HTMLMediaElement) => el.currentTime)).toBeCloseTo(4, 0);
  });
}

test('fullscreen bookmarks and notes stay usable; images keep favorites and notes', async ({ page }) => {
  await open(page);
  await page.locator('#btn-fullscreen').click();
  await expect.poll(() => page.evaluate(() => document.fullscreenElement?.id)).toBe('player');
  await (await control(page, 'btn-save-position')).click();
  await expect(page.locator('#bookmark-label')).toBeFocused();
  await page.locator('#bookmarks-close').click();
  await expect(page.locator('#bookmarks-modal')).toBeHidden();
  await (await control(page, 'btn-player-notes')).click();
  await expect(page.locator('#notes-textarea')).toBeFocused();
  await page.locator('#notes-close').click();
  await expect(page.locator('#notes-modal')).toBeHidden();
  await expect(page.locator('#player')).toHaveClass(/is-fullscreen/);
  await page.locator('#btn-fullscreen').click();
  await expect(page.locator('#player')).not.toHaveClass(/is-fullscreen/);
  await load(page, media.find(item => item.type === 'image')!);
  await expect(page.locator('#btn-save-position')).toBeHidden();
  await expect(page.locator('#btn-bookmarks')).toBeHidden();
  await (await control(page, 'btn-player-favorite')).click();
  await expect(page.locator('#btn-player-favorite')).toHaveAttribute('aria-pressed', /true|false/);
  await (await control(page, 'btn-player-notes')).click();
  await expect(page.locator('#notes-textarea')).toBeFocused();
});

test('detached playback saves and seeks bookmarks and allows native keyboard buttons', async ({ page }) => {
  await open(page);
  const popupEvent = page.waitForEvent('popup');
  await page.keyboard.press('d');
  const popup = await popupEvent;
  await expect(popup.locator('#btn-save-position')).toBeVisible();
  await expect.poll(() => popup.locator('#media-audio').evaluate((el: HTMLMediaElement) => el.readyState)).toBeGreaterThanOrEqual(1);
  await popup.locator('#media-audio').evaluate((el: HTMLMediaElement) => el.pause());
  await popup.locator('#btn-save-position').click();
  await expect(popup.locator('#bookmark-save')).toBeEnabled();
  await popup.locator('#bookmark-label').fill('Detached bookmark');
  await popup.locator('#bookmark-label').press('Enter');
  await expect(popup.locator('.bookmark-jump')).toContainText('Detached bookmark');
  await popup.locator('.bookmark-jump').focus();
  await popup.keyboard.press('Space');
  await expect(popup.locator('#bookmarks-modal')).toBeHidden();
  await popup.locator('#btn-player-notes').click();
  await expect(popup.locator('#notes-textarea')).toBeFocused();
  await popup.keyboard.press('Escape');
  await expect(popup.locator('#btn-player-notes')).toBeFocused();
  await popup.close();
});

test('anonymous shares never render or request personal bookmark, note or favorite controls', async ({ page, browser }) => {
  const item = await open(page);
  const response = await page.request.post(`/api/media/${item.id}/shares`, { data: {} });
  const share = await response.json();
  const context = await browser.newContext({ serviceWorkers: 'block' });
  const shared = await context.newPage();
  const privateRequests: string[] = [];
  shared.on('request', request => {
    if (/\/api\/media\/.*\/(bookmarks|notes|favorite)/.test(request.url())) privateRequests.push(request.url());
  });
  await shared.goto(`${baseURL}/s/${share.token}`);
  await expect(shared.locator('body')).toHaveAttribute('data-share-viewing', 'open');
  await expect(shared.locator('#btn-save-position, #btn-bookmarks, #btn-player-notes, #btn-player-favorite')).toHaveCount(0);
  expect(privateRequests).toEqual([]);
  await context.close();
});
