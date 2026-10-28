/** M30: duplicate policy, atomic multi-target adds and visible membership. */
import { test, expect, request, type Page } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, expectPlaying, captureOutput, assertValidCapture, mainSegment, openPlaylistDetailMoreAction } from './helpers/music-ui';

test.skip(!process.env.PLAYER_M30_ISOLATED, 'Requires an isolated disposable M30 fixture');
test.use({ serviceWorkers: 'block' });
let f: MusicFixture, e: Evidence;
const modal = (page: Page) => page.locator('#playlist-modal');
const list = async (page: Page, id: string) => (await (await page.request.get(`/api/v1/playlists/${id}`)).json());

test.beforeAll(async ({ browser }, info) => {
  f = loadFixture();
  e = new Evidence(process.env.PLAYER_M30_EVIDENCE!, info.project.name,
    { feature: 'M30', browser_version: browser.version(), server_version: f.serverVersion, build_revision: f.buildRevision });
});

test('M30 add/skip across two lists, readable membership and retry', async ({ page, context }, info) => {
  test.setTimeout(180_000);
  const admin = await request.newContext({ baseURL: f.origin });
  expect((await admin.post('/api/v1/auth/login', { data: f.admin })).ok()).toBe(true);
  await page.addInitScript(installAudioTap);
  await keyboardLogin(page, f.user);
  const prefix = `M30 ${info.project.name} ${Date.now()}`;
  const create = async (client: typeof admin, suffix: string, visibility: string) => {
    const response = await client.post('/api/v1/playlists', { data: { name: `${prefix} ${suffix}`, comment: '', visibility } });
    expect(response.ok()).toBe(true);
    return (await response.json()).playlist_id as string;
  };
  const [a, b, shared, privateList] = await Promise.all([
    create(page.request, 'A', 'private'), create(page.request, 'B', 'private'),
    create(admin, 'shared viewer', 'shared'), create(admin, 'private foreign', 'private'),
  ]);
  const first = track(f, 'Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3').id;
  const harbour = track(f, 'Music/Aurora Lane - Night Transit (2021)/03 - Harbour.flac').id;
  try {
    expect((await admin.post(`/api/v1/playlists/${shared}/songs`, { data: { action: 'add', media_ids: [first] } })).ok()).toBe(true);
    expect((await admin.post(`/api/v1/playlists/${privateList}/songs`, { data: { action: 'add', media_ids: [first] } })).ok()).toBe(true);
    await page.goto(`/?music=playlist&id=${a}`);
    await expect(page.getByRole('heading', { name: `${prefix} A`, exact: true })).toBeVisible();
    const add = page.getByRole('button', { name: 'Add songs', exact: true });
    const open = async (song: string) => {
      await add.click();
      const search = modal(page).getByRole('searchbox', { name: 'Find songs to add' });
      await expect(search).toBeFocused();
      await search.fill(song);
      await modal(page).getByRole('checkbox', { name: `Select ${song}`, exact: true }).check();
      await modal(page).getByText('Add to more playlists').click();
      const target = modal(page).getByRole('checkbox', { name: `${prefix} B`, exact: true });
      await target.check();
      await expect(modal(page).getByRole('checkbox', { name: `${prefix} shared viewer`, exact: true })).toHaveCount(0);
      return { search, target };
    };
    let choice = await open('First Light');
    await expect(modal(page).getByRole('combobox', { name: 'Repeated songs' })).toHaveValue('skip');
    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({ path: e.path('multi-target-narrow-dark.png') });
    await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'light'));
    await page.screenshot({ path: e.path('multi-target-narrow-light.png') });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await modal(page).getByRole('button', { name: 'Add selected songs' }).click();
    await expect(modal(page)).not.toHaveClass(/open/);
    expect((await list(page, a)).entries.map((x: any) => x.media_id)).toEqual([first]);
    expect((await list(page, b)).entries.map((x: any) => x.media_id)).toEqual([first]);

    choice = await open('First Light');
    await modal(page).getByRole('button', { name: 'Add selected songs' }).click();
    await expect(modal(page)).not.toHaveClass(/open/);
    expect((await list(page, a)).song_count).toBe(1);
    expect((await list(page, b)).song_count).toBe(1);

    choice = await open('First Light');
    await modal(page).getByRole('combobox', { name: 'Repeated songs' }).selectOption('add');
    await modal(page).getByRole('button', { name: 'Add selected songs' }).click();
    await expect(modal(page)).not.toHaveClass(/open/);
    for (const id of [a, b]) {
      const saved = await list(page, id);
      expect(saved.entries.map((x: any) => x.media_id)).toEqual([first, first]);
      expect(new Set(saved.entries.map((x: any) => x.entry_id)).size).toBe(2);
    }

    await openPlaylistDetailMoreAction(page, 'membership');
    await expect(modal(page)).toContainText(`${prefix} A`);
    await expect(modal(page)).toContainText(`${prefix} B`);
    await expect(modal(page)).toContainText(`${prefix} shared viewer`);
    await expect(modal(page)).not.toContainText(`${prefix} private foreign`);
    await modal(page).getByRole('button', { name: 'Close playlist dialog' }).click();

    choice = await open('Harbour');
    await context.setOffline(true);
    await modal(page).getByRole('button', { name: 'Add selected songs' }).click();
    await expect(modal(page).locator('[data-error]').first()).not.toBeEmpty();
    await expect(choice.target).toBeChecked();
    await expect(modal(page).getByRole('checkbox', { name: 'Select Harbour' })).toBeChecked();
    await context.setOffline(false);
    expect((await list(page, a)).song_count).toBe(2);
    expect((await list(page, b)).song_count).toBe(2);
    await modal(page).getByRole('button', { name: 'Add selected songs' }).click();
    await expect(modal(page)).not.toHaveClass(/open/);
    for (const id of [a, b]) expect((await list(page, id)).entries.map((x: any) => x.media_id)).toEqual([first, first, harbour]);

    const denied = await page.request.post('/api/v1/playlists/songs',
      { data: { playlist_ids: [a, shared], media_ids: [harbour], duplicate_policy: 'add' } });
    expect(denied.status()).toBe(403);
    expect((await list(page, a)).song_count).toBe(3);
    expect((await list(page, shared)).song_count).toBe(1);
    const membership = await (await page.request.get('/api/v1/playlists/membership')).json();
    expect(membership.some((x: any) => x.playlist_id === shared)).toBe(true);
    expect(membership.some((x: any) => x.playlist_id === privateList)).toBe(false);

    await page.locator('.media-row').first().locator('.song-open').click();
    await expectPlaying(page, first);
    const capture = await captureOutput(page, 1200, e.path('duplicate-source.wav'));
    assertValidCapture(capture);
    expect(Math.abs(mainSegment(capture.analysis)!.tone_hz! - 440)).toBeLessThan(5);
    await page.locator('#btn-play').click();
    await add.click();
    await expect(modal(page).getByRole('searchbox', { name: 'Find songs to add' })).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(modal(page)).not.toHaveClass(/open/);
    e.record({ feature: 'M30', scenario: 'two-list skip/add occurrences, readable membership, offline retained retry, atomic permission denial and actual audio', result: 'pass', data: { a, b, shared, privateList, first, harbour }, artifacts: ['duplicate-source.wav', 'multi-target-narrow-dark.png', 'multi-target-narrow-light.png'] });
  } finally {
    await context.setOffline(false);
    await admin.dispose();
  }
});
