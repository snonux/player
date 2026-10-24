import { expect, test } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { assertValidCapture, audioState, captureOutput, keyboardLogin, mainSegment, openFolder, playCard } from './helpers/music-ui';

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

async function measure(page: Parameters<typeof captureOutput>[0], name: string, artifacts: string[]) {
  // The multi-mode journey outlasts a 12-second fixture. Seek through the real
  // timeline before each capture so later settings cannot measure its successor.
  const timeline = page.locator('#progress-track');
  const bounds = (await timeline.boundingBox())!;
  await timeline.click({ position: { x: bounds.width * .02, y: bounds.height / 2 } });
  await expect.poll(async () => (await audioState(page)).time).toBeLessThan(1);
  // One second contains 1,000 cycles of the fixture's 1 kHz tone, enough for
  // the shared analyser while keeping this multi-mode browser journey short.
  const output = await captureOutput(page, 1_200, evidence.path(name));
  assertValidCapture(output);
  artifacts.push(output.wav);
  const segment = mainSegment(output.analysis);
  expect(segment, `audible output for ${name}`).toBeDefined();
  expect(segment!.duration_s, `sustained output for ${name}`).toBeGreaterThan(.5);
  return segment!;
}

async function level(page: Parameters<typeof captureOutput>[0], name: string, artifacts: string[]) {
  return (await measure(page, name, artifacts)).rms_dbfs;
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
    await keyboardLogin(page, fixture.user);
    await openFolder(page, fixture.setIds.Music, GAIN_FOLDER);
    await playCard(page, loud.title, loud.id);

    const mode = page.getByLabel('Loudness', { exact: true });
    const control = page.locator('.loudness-control').filter({ has: mode });
    const status = control.locator('[role="status"]');
    await expect(mode).toBeVisible();
    await mode.selectOption('off');
    const loudOff = await level(page, 'm24-off-loud.wav', artifacts);
    await playAnotherCard(page, quiet.title, quiet.id);
    const quietOff = await level(page, 'm24-off-quiet.wav', artifacts);
    expect(Math.abs(loudOff - quietOff - (loud.level_dbfs - quiet.level_dbfs)), 'Off preserves the known source levels')
      .toBeLessThan(LEVEL_TOLERANCE_DB);
    await playAnotherCard(page, loud.title, loud.id);
    await mode.selectOption('track');
    await expect(status).toContainText('Track tags');
    const loudTrack = await level(page, 'm24-track-loud.wav', artifacts);

    await playAnotherCard(page, quiet.title, quiet.id);
    await expect(status).toContainText('Track tags');
    const quietTrack = await level(page, 'm24-track-quiet.wav', artifacts);
    const normalizedDifference = loudTrack - quietTrack;
    expect(Math.abs(normalizedDifference), 'track tags normalize Loud and Quiet').toBeLessThan(LEVEL_TOLERANCE_DB);
    expect(Math.abs(loudTrack - loudOff + 9), 'Loud track tag attenuates the output by 9 dB').toBeLessThan(LEVEL_TOLERANCE_DB);
    expect(Math.abs(quietTrack - quietOff - 9), 'Quiet track tag boosts the output by 9 dB').toBeLessThan(LEVEL_TOLERANCE_DB);

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
    await preamp.fill('6');
    await preamp.press('Tab');
    const preampPlus = await level(page, 'm24-preamp-plus6.wav', artifacts);
    expect(Math.abs(preampPlus - loudTrack - 6), 'positive preamp changes actual tagged output by +6 dB')
      .toBeLessThan(LEVEL_TOLERANCE_DB);
    await preamp.fill('-6');
    await preamp.press('Tab');
    const preampMinus = await level(page, 'm24-preamp-minus6.wav', artifacts);
    expect(Math.abs(preampMinus - loudTrack + 6), 'negative preamp changes actual tagged output by -6 dB')
      .toBeLessThan(LEVEL_TOLERANCE_DB);

    // Album gain -0.5 + preamp12 requests +11.5 dB on a -3 dBFS peak.
    // Safe output can rise only 3.5 dB from the album baseline. A flattened,
    // clipped sine has a different peak/RMS ratio even if its peak is capped.
    await mode.selectOption('album');
    await preamp.fill('12');
    await preamp.press('Tab');
    await expect(status).toContainText('Gain limited to avoid clipping');
    data.clipping_status = await status.textContent();
    const clipped = await measure(page, 'm24-peak-limited.wav', artifacts);
    expect(Math.abs(clipped.rms_dbfs - loudAlbum - 3.5), 'known peak limits actual output before clipping')
      .toBeLessThan(LEVEL_TOLERANCE_DB);
    expect(clipped.peak_dbfs).toBeGreaterThan(-.5);
    expect(clipped.peak_dbfs).toBeLessThanOrEqual(0);
    expect(clipped.peak_dbfs - clipped.rms_dbfs, 'limited output retains the sine crest factor').toBeGreaterThan(2.5);
    expect(clipped.peak_dbfs - clipped.rms_dbfs).toBeLessThan(3.5);
    data.preamp_dbfs = { plus6: preampPlus, minus6: preampMinus };
    data.peak_limited_output = clipped;

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

    data.levels_dbfs = { loud_off: loudOff, quiet_off: quietOff, loud_track: loudTrack, quiet_track: quietTrack, loud_album: loudAlbum, quiet_album: quietAlbum,
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
