/** Startup/default polling acceptance; restart only an explicitly owned fixture. */
import { test, expect } from '@playwright/test';
import { existsSync, copyFileSync, unlinkSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { join } from 'node:path';
import { Evidence, loadFixture, SUPPORT_DIR } from './helpers/music-fixture';
import { keyboardLogin } from './helpers/music-ui';

test.skip(process.env.PLAYER_PZ3_ISOLATED !== '1', 'Requires an owned default-polling quickscan fixture');

test('startup and default polling discover music without manual rescan', async ({ page, browser }, info) => {
  test.setTimeout(150_000);
  const fixture = loadFixture();
  if (!existsSync(join(fixture.dir, '.player-music-fixture')) || new URL(fixture.origin).hostname !== '127.0.0.1')
    throw Error('Quickscan acceptance needs an owned localhost fixture');
  const evidence = new Evidence(fixture.evidenceDir, info.project.name, {
    browser_version: browser.version(), server_build: fixture.buildRevision, origin: fixture.origin,
  });
  const folder = join(fixture.dir, 'media', 'Music', 'Tag Verification');
  const source = join(folder, 'source_001.flac');
  const prefix = `pz3-${info.project.name}-${Date.now()}`;
  const startup = join(folder, `${prefix}-startup.flac`);
  const watched = join(folder, `${prefix}-watched.flac`);
  const progress = async () => (await page.request.get('/api/v1/admin/scan-progress')).json();
  const rows = async () => (await page.request.get(`/api/v1/media?set_id=${fixture.setIds.Music}&limit=1000`)).json();
  const artifacts: string[] = [];
  try {
    await keyboardLogin(page, fixture.admin);
    const restartWithoutPolling = () => execFileSync('python3', [join(SUPPORT_DIR, 'music_fixture.py'), 'restart', fixture.dir], {
      timeout: 45_000, env: { ...process.env, SCAN_WATCH_SECONDS: '0', SCAN_INTERVAL_MINUTES: '0' },
    });
    // Disable polling and wait for startup to finish before creating the source;
    // only the following restart can discover it.
    restartWithoutPolling();
    await expect.poll(async () => (await progress()).running).toBe(false);
    copyFileSync(source, startup);
    restartWithoutPolling();
    await expect.poll(async () => (await rows()).some((r: any) => r.file_name === `${prefix}-startup.flac`),
      { timeout: 40_000 }).toBe(true);
    await expect.poll(async () => (await progress()).running).toBe(false);
    const initial = await progress();
    expect(initial.watch_seconds).toBe(0);
    expect(initial.trigger).toBe('startup');
    // Unset scan configuration on the next restart to exercise shipped defaults.
    const defaultEnv = { ...process.env };
    delete defaultEnv.SCAN_WATCH_SECONDS;
    delete defaultEnv.SCAN_INTERVAL_MINUTES;
    execFileSync('python3', [join(SUPPORT_DIR, 'music_fixture.py'), 'restart', fixture.dir], {
      timeout: 45_000, env: defaultEnv,
    });
    await expect.poll(async () => {
      const p = await progress();
      return p.trigger === 'watch' && p.state === 'complete' && !p.running && !p.pending;
    }, { timeout: 40_000 }).toBe(true);
    expect((await progress()).watch_seconds).toBe(30);
    expect(initial.interval_minutes).toBe(0);

    copyFileSync(source, watched);
    await expect.poll(async () => (await rows()).some((r: any) => r.file_name === `${prefix}-watched.flac`),
      { timeout: 70_000, intervals: [500, 1000] }).toBe(true);
    await expect.poll(async () => (await progress()).running).toBe(false);
    expect((await progress()).trigger).toBe('watch');
    await page.goto(`/?set=${fixture.setIds.Music}&folder=Tag%20Verification`);
    const arrived = (await rows()).find((r: any) => r.file_name === `${prefix}-watched.flac`);
    await expect(page.locator(`.media-card[data-id="${arrived.id}"]`)).toBeVisible();
    await page.locator('body').click({ position: { x: 20, y: 200 } });
    await page.keyboard.press('Shift+A');
    await page.locator('#admin-section-select').selectOption('scan');
    await page.locator('#admin-scan-options summary').click();
    await expect(page.locator('#admin-scan-settings')).toHaveText('Watch: polling every 30 seconds. Schedule: Off.');
    await expect(page.locator('#admin-scan-options')).toContainText('A quick scan runs at startup');
    artifacts.push(evidence.path('pz3-default-watch.png'));
    await page.screenshot({ path: artifacts[0], animations: 'disabled' });
    await page.setViewportSize({ width: 390, height: 844 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    evidence.record({ feature: 'pz3', scenario: 'startup inventory and default 30-second polling, admin settings and narrow layout', result: 'pass', artifacts });
  } catch (error) {
    evidence.record({ feature: 'pz3', scenario: 'startup inventory and default polling', result: 'fail', detail: String(error), artifacts });
    throw error;
  } finally {
    for (const path of [startup, watched]) if (existsSync(path)) unlinkSync(path);
  }
});
