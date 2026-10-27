/** M37: actual qualified browser plays and account-private history controls. */
import { test, expect, request, type APIRequestContext, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { Evidence, loadFixture, type Account, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, expectPlaying, resetProgress, captureOutput, assertValidCapture, mainSegment, audioState } from './helpers/music-ui';

test.skip(!process.env.PLAYER_M37_ISOLATED, 'Requires exclusive disposable M37 fixture');
test.use({ serviceWorkers: 'block' });
let f: MusicFixture, e: Evidence, ids: { one: number; two: number }, albumKey: string;

async function signedIn(account: Account) {
  const c = await request.newContext({ baseURL: f.origin });
  expect((await c.post('/api/v1/auth/login', { data: account })).ok()).toBe(true);
  return c;
}
async function history(c: APIRequestContext) {
  const r = await c.get('/api/v1/music/history');
  expect(r.ok()).toBe(true);
  return r.json();
}
async function start(page: Page, id: number, keyboard: boolean) {
  await resetProgress(page, id);
  await page.goto('/?music=album&id=' + encodeURIComponent(albumKey));
  const row = page.locator(`.media-row[data-id="${id}"] .song-open`);
  if (keyboard) { await row.focus(); await row.press('Enter'); }
  else await row.click();
  await expectPlaying(page, id);
}
async function pause(page: Page) {
  if (!(await audioState(page)).paused) await page.locator('#btn-play').click();
  await expect.poll(() => audioState(page).then(s => s.paused)).toBe(true);
}

test.beforeAll(async ({ browser }, info) => {
  f = loadFixture();
  const seed = JSON.parse(readFileSync(join(process.env.PLAYER_M37_EVIDENCE!, 'seed.json'), 'utf8'));
  ids = { one: seed.ids['1'], two: seed.ids['2'] };
  const c = await signedIn(f.admin);
  try {
    const albums = await (await c.get('/api/v1/music/albums')).json();
    albumKey = albums.find((a: any) => a.title === seed.album)?.album_key;
    expect(albumKey).toBeTruthy();
  } finally { await c.dispose(); }
  e = new Evidence(process.env.PLAYER_M37_EVIDENCE!, info.project.name,
    { feature: 'M37', browser_version: browser.version(), server_version: f.serverVersion, build_revision: f.buildRevision, ids });
});

test('M37 qualified plays, replay, private settings and history recovery', async ({ page, context }, info) => {
  test.setTimeout(240_000);
  const chrome = info.project.name === 'chrome';
  const actor = chrome ? f.user : f.admin, observer = chrome ? f.admin : f.user;
  const own = await signedIn(actor), other = await signedIn(observer);
  const id = chrome ? ids.one : ids.two;
  const expectedHz = chrome ? 440 : 660;
  const original = await history(own);
  const otherOriginal = await history(other);
  const targetCount = chrome ? 2 : 1;
  const artifacts: string[] = [];
  try {
    await page.addInitScript(installAudioTap);
    await keyboardLogin(page, actor);
    for (let i = 0; i < targetCount; i++) {
      await start(page, id, i === 0);
      const capture = await captureOutput(page, 1200, e.path(`play-${i + 1}.wav`));
      assertValidCapture(capture);
      expect(Math.abs(mainSegment(capture.analysis)!.tone_hz! - expectedHz)).toBeLessThan(5);
      artifacts.push(capture.wav);
      await expect.poll(async () => (await history(own)).events.filter((x: any) => x.media_id === id).length,
        { timeout: 90_000, intervals: [1000] }).toBe(original.events.filter((x: any) => x.media_id === id).length + i + 1);
      await pause(page);
    }
    const after = await history(own);
    expect(after.events.length).toBe(original.events.length + targetCount);
    expect(after.events.slice(0, targetCount).map((x: any) => x.media_id)).toEqual(Array(targetCount).fill(id));
    expect(new Set(after.events.slice(0, targetCount).map((x: any) => x.id)).size).toBe(targetCount);
    expect((await history(other)).events).toEqual(otherOriginal.events);
    const otherId = (await (await other.get('/api/v1/config')).json()).user_id;
    expect((await own.get('/api/v1/music/history?expected_user_id=' + otherId)).status()).toBe(409);

    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Settings', exact: true }).click();
    const panel = page.locator('#settings-modal details.listening-history');
    await panel.locator('summary').click();
    await expect(panel.locator('[data-events] li')).toHaveCount(after.events.length);
    await expect(panel.locator('[data-status]')).toContainText('Recent qualified plays');
    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({ path: e.path('history-narrow-dark.png') });
    await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'light'));
    await page.screenshot({ path: e.path('history-narrow-light.png') });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await panel.locator('[data-days]').selectOption('90');
    await expect(panel.locator('div[data-confirm]')).toBeVisible();
    await panel.locator('[data-cancel]').click();
    await expect(panel.locator('[data-days]')).toHaveValue('365');
    await panel.locator('[data-days]').selectOption('90');
    await panel.locator('button[data-confirm]').click();
    await expect.poll(async () => (await history(own)).settings.retention_days).toBe(90);
    await panel.locator('[data-record]').uncheck();
    await expect.poll(async () => (await history(own)).settings.recording).toBe(false);
    await panel.locator('[data-record]').check();
    await expect.poll(async () => (await history(own)).settings.recording).toBe(true);
    await context.setOffline(true);
    await panel.locator('[data-refresh]').click();
    await expect(panel.locator('[data-status]')).toContainText('Could not load listening history');
    await context.setOffline(false);
    await panel.locator('[data-refresh]').click();
    await expect(panel.locator('[data-events] li')).toHaveCount(after.events.length);

    if (!chrome) {
      await panel.locator('[data-clear]').click();
      await panel.locator('[data-cancel]').click();
      expect((await history(own)).events.length).toBe(after.events.length);
      await panel.locator('[data-clear]').click();
      await panel.locator('button[data-confirm]').click();
      await expect.poll(async () => (await history(own)).events.length).toBe(0);
      expect((await history(other)).events).toEqual(otherOriginal.events);
    }
    e.record({ feature: 'M37', scenario: 'real qualified play/replay, history Settings, privacy and failure recovery', result: 'pass', data: { before: original.events, after: after.events.slice(0, targetCount), other: otherOriginal.events.length, settings: (await history(own)).settings }, artifacts });
  } finally {
    await context.setOffline(false);
    await own.dispose(); await other.dispose();
  }
});
