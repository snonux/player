import { expect, test } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { audioState, expectPlaying, keyboardLogin, openFolder, playCard, resetProgress } from './helpers/music-ui';

const FEATURE = 'B3 Sets audio skips images';
const ALBUM = 'Aurora Lane - Night Transit (2021)';

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

test('B3: last folder audio auto-advances to audio, not cover art, and keeps shortcuts', async ({ page }) => {
  const data: Record<string, unknown> = {};
  const artifacts: string[] = [];
  try {
    const lastAlbumTrack = track(fixture, `Music/${ALBUM}/03 - Harbour.flac`);
    await keyboardLogin(page, fixture.admin);
    await openFolder(page, fixture.setIds.Music, ALBUM);
    await resetProgress(page, lastAlbumTrack.id);
    await playCard(page, lastAlbumTrack.title, lastAlbumTrack.id);
    await expectPlaying(page, lastAlbumTrack.id);

    // A harness seek makes the real media element emit its natural `ended`
    // event quickly; navigation itself remains the production auto-advance.
    await page.locator('#media-audio').evaluate((audio, duration) => {
      audio.currentTime = Math.max(0, Number(duration) - 0.35);
    }, lastAlbumTrack.duration_s);
    await expect.poll(async () => (await audioState(page)).src, { timeout: 15_000 })
      .not.toMatch(new RegExp(`/media/${lastAlbumTrack.id}/`));
    await expect.poll(async () => (await audioState(page)).paused, { timeout: 10_000 }).toBe(false);
    await expect(page.locator('#media-image')).toHaveClass(/hidden/);
    data.after_auto_advance = await audioState(page);

    await page.keyboard.press('Shift+P');
    await expect(page.locator('#media-image')).toHaveClass(/hidden/);
    const afterPrevious = await audioState(page);
    await page.keyboard.press('Shift+N');
    await expect.poll(async () => (await audioState(page)).src).not.toBe(afterPrevious.src);
    await expect(page.locator('#media-image')).toHaveClass(/hidden/);
    data.after_shortcuts = await audioState(page);

    const screenshot = evidence.path('b3-audio-sequence-no-cover-art.png');
    await page.screenshot({ path: screenshot });
    artifacts.push(screenshot);
    evidence.record({ feature: FEATURE, scenario: 'actual end-of-track advance and Next/Previous skip folder cover art', result: 'pass', data, artifacts });
  } catch (error) {
    evidence.record({ feature: FEATURE, scenario: 'actual end-of-track advance and Next/Previous skip folder cover art', result: 'fail', detail: String(error).slice(0, 400), data, artifacts });
    throw error;
  }
});
