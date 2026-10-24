import { expect, test } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { assertValidCapture, captureOutput, keyboardLogin, mainSegment, openFolder, playCard } from './helpers/music-ui';

const FEATURE = 'M24 ReplayGain loudness';
const GAIN_FOLDER = 'Gain Check';
const LEVEL_TOLERANCE_DB = 1.5;

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

test.beforeEach(async ({ page }) => {
  await page.addInitScript(installAudioTap);
});

async function level(page: Parameters<typeof captureOutput>[0], name: string, artifacts: string[]) {
  // One second contains 1,000 cycles of the fixture's 1 kHz tone, enough for
  // the shared analyser while keeping this multi-mode browser journey short.
  const output = await captureOutput(page, 1_200, evidence.path(name));
  assertValidCapture(output);
  artifacts.push(output.wav);
  const value = mainSegment(output.analysis)?.rms_dbfs;
  expect(value, `measured level for ${name}`).toBeDefined();
  return value!;
}

/** Keep the floating player from covering cards near the bottom of the grid. */
async function playAnotherCard(page: Parameters<typeof playCard>[0], title: string, id: number) {
  await page.locator('#btn-minimize').click();
  await playCard(page, title, id);
  await page.getByRole('button', { name: 'Restore player', exact: true }).click();
}

test('M24: loudness settings normalize tagged music and keep untagged music clipping-safe', async ({ page }) => {
  const data: Record<string, unknown> = {};
  const artifacts: string[] = [];
  try {
    const loud = track(fixture, 'Music/Gain Check/Loud.flac');
    const quiet = track(fixture, 'Music/Gain Check/Quiet.flac');
    const untagged = track(fixture, 'Music/Gain Check/No Gain Tags.mp3');
    await keyboardLogin(page, fixture.admin);
    await openFolder(page, fixture.setIds.Music, GAIN_FOLDER);
    await playCard(page, loud.title, loud.id);

    const mode = page.getByLabel('Loudness', { exact: true });
    const control = page.locator('.loudness-control').filter({ has: mode });
    const status = control.locator('[role="status"]');
    await expect(mode).toBeVisible();
    await mode.selectOption('track');
    await expect(status).toContainText('Track tags');
    const loudTrack = await level(page, 'm24-track-loud.wav', artifacts);

    await playAnotherCard(page, quiet.title, quiet.id);
    await expect(status).toContainText('Track tags');
    const quietTrack = await level(page, 'm24-track-quiet.wav', artifacts);
    const normalizedDifference = loudTrack - quietTrack;
    expect(Math.abs(normalizedDifference), 'track tags normalize Loud and Quiet').toBeLessThan(LEVEL_TOLERANCE_DB);

    await mode.selectOption('album');
    await expect(status).toContainText('Album tags');
    const quietAlbum = await level(page, 'm24-album-quiet.wav', artifacts);
    await playAnotherCard(page, loud.title, loud.id);
    await expect(status).toContainText('Album tags');
    const loudAlbum = await level(page, 'm24-album-loud.wav', artifacts);
    const albumDifference = loudAlbum - quietAlbum;
    expect(Math.abs(albumDifference - (loud.level_dbfs - quiet.level_dbfs)), 'album tags preserve album dynamics')
      .toBeLessThan(LEVEL_TOLERANCE_DB);

    // Outside an album queue Automatic deliberately falls back to track tags.
    await mode.selectOption('automatic');
    await expect(status).toContainText('Track tags');
    const loudAutomatic = await level(page, 'm24-automatic-track-loud.wav', artifacts);
    expect(Math.abs(loudAutomatic - loudTrack), 'Automatic uses track tags outside an album queue')
      .toBeLessThan(LEVEL_TOLERANCE_DB);
    data.automatic_fallback = await status.textContent();

    const advanced = control.locator('details');
    await advanced.locator('summary').press('Enter');
    const preamp = control.getByLabel('Loudness preamp in decibels');
    await preamp.fill('12');
    await preamp.press('Tab');
    await expect(status).toContainText('Gain limited to avoid clipping');
    data.clipping_status = await status.textContent();

    await mode.selectOption('track');
    await playAnotherCard(page, untagged.title, untagged.id);
    const untaggedGain = control.getByLabel('Untagged gain in decibels');
    await untaggedGain.fill('12');
    await untaggedGain.press('Tab');
    await expect(status).toContainText('Peak unknown; positive boost disabled');
    const untaggedSafe = await level(page, 'm24-untagged-safe.wav', artifacts);
    await mode.selectOption('off');
    const untaggedOff = await level(page, 'm24-untagged-off.wav', artifacts);
    expect(Math.abs(untaggedSafe - untaggedOff), 'untagged audio is never positively boosted').toBeLessThan(LEVEL_TOLERANCE_DB);

    data.levels_dbfs = { loud_track: loudTrack, quiet_track: quietTrack, loud_album: loudAlbum, quiet_album: quietAlbum,
      loud_automatic: loudAutomatic,
      untagged_safe: untaggedSafe, untagged_off: untaggedOff };
    data.track_difference_db = normalizedDifference;
    data.album_difference_db = albumDifference;
    const screenshot = evidence.path('m24-loudness-advanced.png');
    await page.screenshot({ path: screenshot });
    artifacts.push(screenshot);
    evidence.record({ feature: FEATURE, scenario: 'real tagged and untagged playback through the loudness gain node', result: 'pass', data, artifacts });
  } catch (error) {
    evidence.record({ feature: FEATURE, scenario: 'real tagged and untagged playback through the loudness gain node', result: 'fail', detail: String(error).slice(0, 400), data, artifacts });
    throw error;
  }
});
