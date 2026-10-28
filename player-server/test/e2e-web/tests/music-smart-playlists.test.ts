/** M33: dynamic rules in the installed browser against a disposable server. */
import { randomBytes } from 'node:crypto';
import { test, expect, request, type Page } from '@playwright/test';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, expectPlaying, captureOutput, assertValidCapture, mainSegment, openPlaylistNewAction, openPlaylistDetailMoreAction } from './helpers/music-ui';

test.skip(!process.env.PLAYER_M33_ISOLATED, 'Requires isolated disposable M33 fixture');
test.use({ serviceWorkers: 'block' });
let f: MusicFixture, e: Evidence;
const modal = '#playlist-modal';
const rules = (p: Page) => p.locator(`${modal} [data-rule-form]`);
const detail = (p: Page) => p.locator('#media-grid');
const playlist = async (p: Page, id: string) => (await (await p.request.get(`/api/v1/playlists/${id}`)).json());
const ids = (p: any) => p.songs.map((song: any) => song.id);

async function editRule(page: Page, field: string, operator: string, value: string, sort = 'title', limit = '2') {
  await openPlaylistDetailMoreAction(page, 'rules');
  const form = rules(page);
  await expect(form).toBeVisible();
  const row = form.locator('[data-row="0"]');
  await row.locator('[data-field]').selectOption(field);
  await row.locator('[data-operator]').selectOption(operator);
  if (field === 'starred') await row.locator('[data-value]').selectOption(value);
  else await row.locator('[data-value]').fill(value);
  await form.locator('[data-sort]').selectOption(sort);
  await form.locator('[data-limit]').fill(limit);
  await form.getByRole('button', { name: 'Save rules' }).click();
  await expect(form).toBeHidden();
  await detail(page).locator('details.playlist-meta').evaluate((el: HTMLDetailsElement) => { el.open = true; });
  await expect(detail(page).locator('[data-rule-summary]')).toBeVisible();
}

test.beforeAll(async ({ browser }, info) => {
  f = loadFixture();
  e = new Evidence(process.env.PLAYER_M33_EVIDENCE!, info.project.name,
    { feature: 'M33', browser_version: browser.version(), server_version: f.serverVersion, build_revision: f.buildRevision });
});

test('M33 smart rules refresh with metadata, rating, favorite, plays and dates', async ({ page, context }, info) => {
  test.setTimeout(180_000);
  const actor = info.project.name === 'chrome' ? f.admin : f.user;
  const observer = info.project.name === 'chrome' ? f.user : f.admin;
  await page.addInitScript(installAudioTap);
  await keyboardLogin(page, actor);
  await page.goto('/?music=playlists');
  const name = `M33 ${info.project.name} ${Date.now()}`;
  await openPlaylistNewAction(page, 'smart');
  await expect(page.locator(modal)).toHaveClass(/open/);
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
  await page.locator(`${modal} [name=visibility]`).selectOption('shared');
  await page.locator(modal).getByRole('button', { name: 'Create playlist' }).click();
  await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
  const id = new URL(page.url()).searchParams.get('id')!;
  const form = rules(page);
  await expect(form).toBeVisible();
  await expect(form.locator('[data-enabled]')).toBeChecked();
  await form.locator('[data-row="0"] [data-value]').fill('Synthwave');
  await form.locator('[data-limit]').fill('2');
  await form.getByRole('button', { name: 'Save rules' }).click();
  await expect(form).toBeHidden();
  const metadata = await playlist(page, id);
  expect(metadata.song_count).toBe(2);
  expect(ids(metadata)).toEqual([...ids(metadata)].sort((a: number, b: number) => {
    const x = metadata.songs.find((song: any) => song.id === a).music.title;
    const y = metadata.songs.find((song: any) => song.id === b).music.title;
    return x.localeCompare(y);
  }));
  await expect(detail(page).locator('.media-row')).toHaveCount(2);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: e.path('smart-narrow-dark.png') });
  await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'light'));
  await page.screenshot({ path: e.path('smart-narrow-light.png') });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await detail(page).locator('.media-row').first().locator('.song-open').click();
  await expectPlaying(page, metadata.songs[0].id);
  const capture = await captureOutput(page, 1200, e.path('smart-source.wav'));
  assertValidCapture(capture);
  const source = f.tracks.find(t => f.mediaIds[t.path] === metadata.songs[0].id)!;
  expect(Math.abs(mainSegment(capture.analysis)!.tone_hz! - source.tone_hz[0])).toBeLessThan(5);
  await page.locator('#btn-play').click();

  const target = metadata.songs[info.project.name === 'chrome' ? 0 : 1].id as number;
  await editRule(page, 'rating', 'gte', '4');
  await expect(detail(page).locator('.media-row')).toHaveCount(0);
  expect((await page.request.put(`/api/v1/music/ratings/song/${target}`, { data: { rating: 5 } })).ok()).toBe(true);
  await openPlaylistDetailMoreAction(page, 'refresh');
  await expect(detail(page).locator('.media-row')).toHaveCount(1);
  expect(ids(await playlist(page, id))).toEqual([target]);

  await editRule(page, 'starred', 'is', 'true');
  await expect(detail(page).locator('.media-row')).toHaveCount(0);
  expect((await page.request.post(`/api/v1/media/${target}/favorite`)).ok()).toBe(true);
  await openPlaylistDetailMoreAction(page, 'refresh');
  await expect(detail(page).locator('.media-row')).toHaveCount(1);
  expect(ids(await playlist(page, id))).toEqual([target]);

  await editRule(page, 'last_played', 'within_days', '1');
  await expect(detail(page).locator('.media-row')).toHaveCount(0);
  const event = randomBytes(16).toString('hex');
  const sync = await page.request.post(`/api/v1/media/${target}/playback-sync`,
    { data: { music_plays: [{ id: event, observed_ms: Date.now(), accumulated_seconds: 60 }] } });
  expect(sync.ok()).toBe(true);
  expect((await sync.json()).accepted_music_plays).toContain(event);
  await openPlaylistDetailMoreAction(page, 'refresh');
  await expect(detail(page).locator('.media-row')).toHaveCount(1);
  expect(ids(await playlist(page, id))).toEqual([target]);

  await editRule(page, 'plays', 'gte', '1');
  expect(ids(await playlist(page, id))).toEqual([target]);
  await editRule(page, 'added', 'within_days', '1', 'title', '1');
  await expect(detail(page).locator('.media-row')).toHaveCount(1);
  const added = await playlist(page, id);
  expect(added.song_count).toBe(1);

  const other = await request.newContext({ baseURL: f.origin });
  try {
    expect((await other.post('/api/v1/auth/login', { data: observer })).ok()).toBe(true);
    expect((await other.get(`/api/v1/playlists/${id}`)).ok()).toBe(true);
    if (info.project.name === 'chrome')
      expect((await other.put(`/api/v1/playlists/${id}/rules`, { data: { rules: metadata.rules } })).status()).toBe(403);
    await editRule(page, 'rating', 'gte', '4');
    expect(ids(await playlist(page, id))).toEqual([target]);
    const foreign = await (await other.get(`/api/v1/playlists/${id}`)).json();
    expect(foreign.song_count).toBe(0);
  } finally { await other.dispose(); }

  await openPlaylistDetailMoreAction(page, 'rules');
  await expect(rules(page).locator('[data-enabled]')).toBeFocused();
  await rules(page).locator('[data-row="0"] [data-value]').fill('3');
  await context.setOffline(true);
  await rules(page).getByRole('button', { name: 'Save rules' }).click();
  await expect(rules(page).getByRole('alert')).not.toBeEmpty();
  await expect(rules(page).locator('[data-row="0"] [data-value]')).toHaveValue('3');
  await context.setOffline(false);
  await page.keyboard.press('Escape');
  await expect(rules(page)).toBeHidden();
  expect((await playlist(page, id)).rules.conditions[0].value).toBe('4');
  // Keep the shared fixture's next browser account free of this run's stars.
  expect((await page.request.put(`/api/v1/music/ratings/song/${target}`, { data: { rating: 0 } })).ok()).toBe(true);
  const unfavorite = await page.request.post(`/api/v1/media/${target}/favorite`);
  expect(unfavorite.ok()).toBe(true);
  expect((await unfavorite.json()).favorite).toBe(false);
  e.record({ feature: 'M33', scenario: 'real UI smart rule creation, dynamic personal and UTC date refresh, ordered limit, audio, permissions and offline draft', result: 'pass', data: { id, metadata: ids(metadata), target, added: ids(added), event }, artifacts: ['smart-source.wav', 'smart-narrow-dark.png', 'smart-narrow-light.png'] });
});
