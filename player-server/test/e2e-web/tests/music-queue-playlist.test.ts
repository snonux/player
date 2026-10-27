/** M28: save the actual playing queue, including duplicate occurrences. */
import { test, expect, request, type Page } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, expectPlaying, audioState, captureOutput, assertValidCapture, mainSegment } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires disposable local music fixture');
let f: MusicFixture, e: Evidence;
const panel = (p: Page) => p.locator('#queue-panel');
const rows = (p: Page) => panel(p).locator('[data-queue-index]');
const ids = () => [
  track(f, 'Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3').id,
  track(f, 'Music/Aurora Lane - Night Transit (2021)/02 - Long Road Home.mp3').id,
];

test.beforeAll(async ({ browser }, info) => {
  f = loadFixture();
  e = new Evidence(process.env.PLAYER_M28_EVIDENCE || f.evidenceDir, info.project.name,
    { feature: 'M28', browser_version: browser.version(), server_version: f.serverVersion, build_revision: f.buildRevision });
});
test.beforeEach(async ({ page }) => {
  await page.addInitScript(installAudioTap);
  await keyboardLogin(page, f.user);
});

async function seedQueue(page: Page, name: string) {
  const [a, b] = ids();
  const create = await page.request.post('/api/v1/playlists', { data: { name, visibility: 'private' } });
  expect(create.ok()).toBe(true);
  const id = (await create.json()).playlist_id as string;
  expect((await page.request.post(`/api/v1/playlists/${id}/songs`, { data: { action: 'add', media_ids: [a, b, a] } })).ok()).toBe(true);
  await page.goto('/?music=playlist&id=' + encodeURIComponent(id));
  await expect(page.locator('.media-row')).toHaveCount(3);
  await page.locator('.media-row').first().click();
  await expectPlaying(page, a);
  await page.keyboard.press('Q');
  await expect(rows(page)).toHaveCount(3);
  expect(await panel(page).locator('.queue-track').evaluateAll(nodes => nodes.map(n => n.childNodes[0].textContent))).toEqual(['First Light', 'Long Road Home', 'First Light']);
  return id;
}

test('M28 named private playlist copies the full queue while playback continues', async ({ page }) => {
  test.setTimeout(120_000);
  const admin = await request.newContext({ baseURL: f.origin });
  await admin.post('/api/v1/auth/login', { data: f.admin });
  let seed = '', saved = '', deleted = false;
  const [a, b] = ids();
  try {
    seed = await seedQueue(page, 'M28 seed ' + Date.now());
    const before = await audioState(page);
    const saveButton = panel(page).getByRole('button', { name: 'Save as playlist' });
    await saveButton.focus(); await page.keyboard.press('Enter');
    const form = page.getByRole('dialog', { name: 'Save queue as playlist' });
    await expect(form).toBeVisible();
    await expect(form.getByRole('textbox', { name: 'Playlist name' })).toBeFocused();
    await expect(form).toContainText('3 songs · Private playlist');
    await page.keyboard.press('Escape');
    await expect(form).toHaveCount(0);
    await expect(saveButton).toBeFocused();
    await saveButton.press('Enter');
    await expect(form.getByRole('textbox', { name: 'Playlist name' })).toBeFocused();
    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({ path: e.path('save-dialog-narrow-dark.png') });
    await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'light'));
    await page.screenshot({ path: e.path('save-dialog-narrow-light.png') });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    const name = 'M28 saved ' + Date.now();
    await form.getByRole('textbox', { name: 'Playlist name' }).fill(name);
    expect((await admin.delete('/api/v1/media/' + b)).ok()).toBe(true);
    deleted = true;
    const rejected = page.waitForResponse(r => r.url().endsWith('/api/playlists') && r.request().method() === 'POST');
    await form.getByRole('textbox', { name: 'Playlist name' }).press('Enter');
    expect((await rejected).status()).toBeGreaterThanOrEqual(400);
    await expect(form.getByRole('alert')).not.toBeEmpty();
    await expect(form.getByRole('textbox', { name: 'Playlist name' })).toHaveValue(name);
    expect((await (await page.request.get('/api/v1/playlists')).json()).some((p: any) => p.name === name)).toBe(false);
    expect((await audioState(page)).src).toBe(before.src);
    expect((await admin.post('/api/v1/media/' + b + '/restore')).ok()).toBe(true);
    deleted = false;
    await form.getByRole('textbox', { name: 'Playlist name' }).press('Enter');
    await expect(form.getByRole('link', { name: 'Open playlist' })).toBeFocused();
    const list = await (await page.request.get('/api/v1/playlists')).json();
    saved = list.find((p: any) => p.name === name)?.playlist_id;
    expect(saved).toBeTruthy();
    const detail = await (await page.request.get('/api/v1/playlists/' + saved)).json();
    expect(detail.visibility).toBe('private');
    expect(detail.entries.map((entry: any) => entry.media_id)).toEqual([a, b, a]);
    const after = await audioState(page);
    expect(after.src).toBe(before.src);
    expect(after.paused).toBe(false);
    expect(after.time).toBeGreaterThan(before.time);
    const capture = await captureOutput(page, 1200, e.path('save-keeps-playing.wav'));
    assertValidCapture(capture);
    expect(Math.abs(mainSegment(capture.analysis)!.tone_hz! - 440)).toBeLessThan(5);
    await form.getByRole('link', { name: 'Open playlist' }).click();
    await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
    await expect(page.locator('.media-row')).toHaveCount(3);
    expect((await audioState(page)).src).toBe(before.src);

    expect((await admin.delete('/api/v1/media/' + b)).ok()).toBe(true);
    deleted = true;
    await page.reload();
    await expect(page.locator('.media-row')).toHaveCount(2);
    const filtered = await (await page.request.get('/api/v1/playlists/' + saved)).json();
    expect(filtered.entries.map((entry: any) => entry.media_id)).toEqual([a, a]);
    e.record({ feature: 'M28', scenario: 'private duplicate queue save, live source, open and deleted-song filtering', result: 'pass', data: { saved, before, after, entries: detail.entries.map((x: any) => x.media_id), filtered: filtered.entries.map((x: any) => x.media_id), audio: mainSegment(capture.analysis) } });
  } finally {
    if (deleted) await admin.post('/api/v1/media/' + b + '/restore');
    if (saved) await page.request.delete('/api/v1/playlists/' + saved);
    if (seed) await page.request.delete('/api/v1/playlists/' + seed);
    await admin.dispose();
  }
});
