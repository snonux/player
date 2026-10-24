import { expect, test } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { audioState, expectPlaying, keyboardLogin, openFolder, playCard } from './helpers/music-ui';

const FEATURE = 'F01 timed lyric seeking';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'PLAYER_MUSIC_FIXTURE is not set (see music-e2e-verification.md)');

let fixture: MusicFixture;
let evidence: Evidence;

test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  evidence = new Evidence(fixture.evidenceDir, info.project.name, {
    browser: info.project.name,
    browser_version: browser.version(),
    channel: info.project.use.channel,
    server_version: fixture.serverVersion,
    server_build_revision: fixture.buildRevision,
    checkout: fixture.checkout,
    origin: fixture.origin,
    run_id: fixture.runId,
  });
});

test('F01: timed lines seek by keyboard and pointer without breaking plain lyrics or offline playback', async ({ page }) => {
  const data: Record<string, unknown> = {};
  const artifacts: string[] = [];
  try {
    const timed = track(fixture, 'Music/Aurora Lane - Night Transit (2021)/02 - Long Road Home.mp3');
    const plain = track(fixture, 'Music/Ünïcødé Artïst - 東京 Nights/01 - Café Ñoño.mp3');
    await page.setViewportSize({ width: 390, height: 844 });
    await keyboardLogin(page, fixture.admin);
    await openFolder(page, fixture.setIds.Music, 'Aurora Lane - Night Transit (2021)');
    await playCard(page, timed.title, timed.id);
    await expectPlaying(page, timed.id);

    const panel = page.getByRole('region', { name: 'Lyrics' });
    await expect(panel).toBeVisible();
    const middle = panel.getByRole('button', { name: /Seek to 0:04: Every mile a quiet song/ });
    const last = panel.getByRole('button', { name: /Seek to 0:08: Harbour lights to guide me on/ });
    await expect(middle).toBeVisible();
    await middle.focus();
    await page.keyboard.press('Enter');
    await expect.poll(async () => (await audioState(page)).time).toBeGreaterThanOrEqual(4);
    await expect(middle).toHaveAttribute('aria-current', 'true');
    await last.click();
    await expect.poll(async () => (await audioState(page)).time).toBeGreaterThanOrEqual(8);
    await expect(last).toHaveAttribute('aria-current', 'true');
    const timedShot = evidence.path('f01-timed-seek-narrow.png');
    await page.screenshot({ path: timedShot });
    artifacts.push(timedShot);

    // The already loaded lyrics must keep seeking locally while the server is
    // unavailable; the current source must remain the same.
    const sourceBeforeOffline = (await audioState(page)).src;
    await page.context().setOffline(true);
    await middle.click();
    await expect.poll(async () => (await audioState(page)).time).toBeGreaterThanOrEqual(4);
    expect((await audioState(page)).src).toBe(sourceBeforeOffline);
    await page.context().setOffline(false);

    await openFolder(page, fixture.setIds.Music, 'Ünïcødé Artïst - 東京 Nights');
    await playCard(page, plain.title, plain.id);
    await expectPlaying(page, plain.id);
    await expect(panel).toContainText('Café au lait, ñoño día');
    await expect(panel.getByRole('button')).toHaveCount(0);
    const plainShot = evidence.path('f01-plain-lyrics.png');
    await page.screenshot({ path: plainShot });
    artifacts.push(plainShot);
    data.keyboard_seek = 4;
    data.pointer_seek = 8;
    data.offline_seek = true;
    data.plain_rows_are_not_actions = true;
    evidence.record({ feature: FEATURE, scenario: 'narrow timed keyboard/pointer seek, offline loaded seek, and plain lyric fallback', result: 'pass', data, artifacts });
  } catch (error) {
    evidence.record({ feature: FEATURE, scenario: 'narrow timed keyboard/pointer seek, offline loaded seek, and plain lyric fallback', result: 'fail', detail: String(error).slice(0, 400), data, artifacts });
    throw error;
  }
});
