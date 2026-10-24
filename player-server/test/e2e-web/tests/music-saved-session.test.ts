import { writeFileSync } from 'node:fs';
import { test, expect, type Page } from '@playwright/test';
import { Evidence, loadFixture, track } from './helpers/music-fixture';
import { keyboardLogin, expectPlaying, audioState } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Disposable music fixture required');
const panel = (p: Page) => p.locator('#queue-panel');
const qstate = (p: Page) => p.evaluate(async () => (await import('/js/queue.js')).queueState());
const remote = async (p: Page) => (await p.request.get('/api/v1/queue')).json();
const button = (p: Page, name: string) => panel(p).getByRole('button', { name, exact: true });
async function action(p: Page, name: string, accept = true) {
  p.once('dialog', dialog => accept ? dialog.accept() : dialog.dismiss());
  await button(p, name).click();
}

test('M17 explicit cross-client continuation, durable choices, offline conflict and late response safety', async ({ page, browser }, info) => {
  const f = loadFixture();
  const evidence = new Evidence(f.evidenceDir, info.project.name, { browser_version: browser.version(), server_version: f.serverVersion, build: f.buildRevision });
  const a = track(f, 'Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3');
  const b = track(f, 'Music/Aurora Lane - Night Transit (2021)/02 - Long Road Home.mp3');
  let playlist: number | undefined;
  const otherContext = await browser.newContext({ baseURL: f.origin });
  const other = await otherContext.newPage();
  const artifacts: string[] = [];
  const syncDiagnostics: any[] = [];
  for (const [name, client] of [['source', page], ['destination', other]] as const) client.on('response', async response => {
    if (response.url().endsWith('/api/v1/queue')) syncDiagnostics.push({ client: name, method: response.request().method(), status: response.status(), revision: await response.json().then(body => body.revision).catch(() => null) });
  });
  try {
    await page.bringToFront();
    await keyboardLogin(page, f.admin);
    playlist = (await (await page.request.post('/api/v1/playlists', { data: { name: `M17 transfer ${Date.now()}` } })).json()).playlist_id;
    expect((await page.request.post(`/api/v1/playlists/${playlist}/songs`, { data: { action: 'add', media_ids: [a.id, b.id, a.id] } })).ok()).toBe(true);
    await page.goto(`/?music=playlist&id=${playlist}`);
    await expect(page.locator('.media-row')).toHaveCount(3);
    await page.locator('.media-row').nth(2).click();
    await expectPlaying(page, a.id);
    await page.locator('#btn-play').click();
    await page.keyboard.press('Q');
    await page.keyboard.press('K');
    expect((await qstate(page)).index).toBe(1);
    await page.keyboard.press('Escape');
    const progress = page.locator('#progress-track');
    const box = (await progress.boundingBox())!;
    await progress.click({ position: { x: box.width * .35, y: Math.min(5, box.height / 2) } });
    await page.keyboard.press('Q');
    await action(page, 'Replace saved session');
    await expect.poll(async () => (await remote(page)).position_seconds).toBeGreaterThan(3);
    const saved = await remote(page);
    expect(saved.entries.map((entry: any) => entry.media_id)).toEqual([a.id, a.id, b.id]);
    expect(saved.current_id).toBe(saved.entries[1].id);
    expect(saved.entries[0].id).not.toBe(saved.entries[1].id);

    await other.bringToFront();
    await keyboardLogin(other, f.admin);
    await other.keyboard.press('Q');
    await button(other, 'Continue session').click();
    await expect(button(other, 'Resume queue')).toBeVisible();
    const transferred = await qstate(other);
    expect(transferred.entries.map((entry: any) => entry.id)).toEqual(saved.entries.map((entry: any) => entry.id));
    expect(transferred.index).toBe(1);
    expect(transferred.position).toBeCloseTo(saved.position_seconds, 1);
    expect((await audioState(other)).paused).toBe(true);
    await expect.poll(() => other.evaluate(() => Boolean(document.activeElement?.closest('#queue-panel')))).toBe(true);
    for (const [theme, width] of [['dark', 1366], ['light', 390]] as const) {
      await other.setViewportSize({ width, height: 844 });
      if (await other.locator('html').getAttribute('data-theme') !== theme) {
        await button(other, 'Close queue').click();
        await other.getByRole('button', { name: 'Toggle theme', exact: true }).click();
        await other.keyboard.press('Q');
      }
      const path = evidence.path(`m17-continued-${theme}-${width}.png`);
      await other.screenshot({ path, animations: 'disabled' }); artifacts.push(path);
      expect(await other.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    }
    await button(other, 'Resume queue').click();
    const resumed = await expectPlaying(other, a.id);
    expect(resumed.time).toBeGreaterThan(saved.position_seconds);
    await other.keyboard.press('Escape');
    if ((await other.locator('#player').getAttribute('class') || '').includes('minimized')) await other.getByRole('button', { name: 'Restore player', exact: true }).click();
    await other.locator('#btn-play').click();
    await other.keyboard.press('Q');

    // While offline the original client edits its own queue. A different client
    // saves a newer revision; reconnect must offer a choice and preserve both.
    await page.context().setOffline(true);
    await page.keyboard.press('K');
    const offline = await qstate(page);
    // Resume/Pause can still have a progress mutation in flight. Refresh the
    // destination's known CAS revision after the source is actually offline,
    // then make the competing structural edit from that current revision.
    await action(other, 'Continue session');
    await expect(button(other, 'Resume queue')).toBeVisible();
    const revision = (await remote(other)).revision;
    await panel(other).locator('[data-queue-action="repeat"]').click();
    await action(other, 'Replace saved session');
    await expect.poll(async () => (await remote(other)).revision).toBeGreaterThan(revision);
    await page.context().setOffline(false);
    await expect(button(page, 'Keep other session')).toBeVisible({ timeout: 30000 });
    const kept = await remote(other);
    await button(page, 'Keep other session').click();
    expect((await qstate(page)).entries.map((entry: any) => entry.id)).toEqual(offline.entries.map((entry: any) => entry.id));
    await page.waitForTimeout(700);
    expect((await remote(other)).revision).toBe(kept.revision);
    await action(page, 'Replace saved session');
    await expect.poll(async () => (await remote(page)).revision).toBeGreaterThan(kept.revision);
    const replaced = await remote(page);
    expect(replaced.entries.map((entry: any) => entry.id)).toEqual(offline.entries.map((entry: any) => entry.id));

    // Delay a real server response, then give the player a newer trusted intent.
    // Releasing the saved queue must never overwrite that playing occurrence.
    let release!: () => void;
    let started!: () => void;
    const waiting = new Promise<void>(resolve => { started = resolve; });
    const gate = new Promise<void>(resolve => { release = resolve; });
    await other.route('**/api/v1/queue', async route => {
      if (route.request().method() !== 'GET') return route.continue();
      const response = await route.fetch(); started(); await gate;
      await route.fulfill({ response });
    });
    await action(other, 'Continue session');
    await waiting;
    await panel(other).locator('[data-queue-index="2"]').first().getByRole('button', { name: `Play ${b.title}`, exact: true }).click();
    await expectPlaying(other, b.id);
    const active = await qstate(other);
    release();
    await expect(button(other, 'Continue session')).toBeEnabled();
    await other.waitForTimeout(700);
    expect((await qstate(other)).entries.map((entry: any) => entry.id)).toEqual(active.entries.map((entry: any) => entry.id));
    expect((await qstate(other)).index).toBe(2);
    expect((await audioState(other)).paused).toBe(false);
    await other.unroute('**/api/v1/queue');
    await other.keyboard.press('Escape');
    if ((await other.locator('#player').getAttribute('class') || '').includes('minimized')) await other.getByRole('button', { name: 'Restore player', exact: true }).click();
    await other.locator('#btn-play').click();
    await other.keyboard.press('/');
    const search = other.getByRole('searchbox', { name: 'Search media' });
    await search.fill('r/h/l/? Q J K'); await expect(search).toHaveValue('r/h/l/? Q J K');

    // Leave the explicit browser save for native Continue, rather than a playing
    // client's later progress. The native runner compares this authoritative JSON.
    await action(page, 'Replace saved session');
    await expect.poll(async () => (await remote(page)).entries.map((entry: any) => entry.id)).toEqual(offline.entries.map((entry: any) => entry.id));
    const final = await remote(page);
    writeFileSync(evidence.path('m17-browser-saved-session.json'), JSON.stringify(final, null, 2));
    evidence.record({ feature: 'M17', scenario: 'two actual clients continue duplicate occurrence paused offset; resume source/progress; offline conflict Keep/Replace; late response preserves active playback; keyboard typing and layouts', result: 'pass', data: { saved, transferred, resumed, replaced, final }, artifacts });
  } catch (error) {
    evidence.record({ feature: 'M17', scenario: 'server queue continuity', result: 'fail', detail: String(error), data: { syncDiagnostics, source: await qstate(page).catch(() => null), destination: await qstate(other).catch(() => null), destinationSync: await other.evaluate(() => Object.fromEntries(Object.entries(localStorage).filter(([key]) => key.endsWith('.server')))).catch(() => null) }, artifacts }); throw error;
  } finally {
    await otherContext.close();
    if (playlist) await page.request.delete(`/api/v1/playlists/${playlist}`);
  }
});
