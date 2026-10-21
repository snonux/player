/**
 * music-baseline.test.ts — V01 (task oj3) baseline journeys on the music fixture.
 *
 * Runs in installed Google Chrome and Mozilla Firefox (playwright.music.config.ts)
 * against a disposable server from music_fixture.py. It proves the harness
 * works on today's Player, not any future music feature:
 *
 * 1. sign-in refuses a wrong password; typed text reaches the fields intact;
 * 2. a keyboard-only journey (admin, desktop, dark): open the Music set and an
 *    album folder with 1/l/Enter, play with Enter, open and close help with
 *    ?/Escape (focus returns to the card), type in search without shortcuts
 *    firing, and pause/resume with p;
 * 3. a touch journey in Chrome, a mouse journey in Firefox (regular user,
 *    390 px wide, light theme): the sidebar lists only the granted set, a
 *    tapped card opens the action menu whose Play button plays (with the
 *    mouse: the card's ▶ button); the restricted set's media is refused;
 * 4. audio-output calibration: tones of known frequency and level are
 *    identified in the captured output, two ReplayGain fixture tracks differ
 *    by their real 18 dB, the EQ probe's three bands are measured, and the
 *    gap at an automatic track change in the continuous "Gapless Suite" is
 *    measured and recorded as today's baseline (not asserted gapless).
 *
 * Every check appends a record to <evidence>/<browser>/evidence.jsonl with
 * screenshots and WAV captures beside it.
 */
import { test, expect, type Locator, type Page } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import {
  audioState, captureOutput, expectPlaying, keyboardLogin, mainSegment, openFolder, playCard, resetProgress, selectByKeys,
} from './helpers/music-ui';

const FEATURE = 'V01 baseline';
const ALBUM = 'Aurora Lane - Night Transit (2021)';
// Measurement tolerances. Tone frequency: the zero-crossing estimate is exact
// to well under 1 Hz; 2 % absorbs browser resampling. Levels: lossless
// sources through an unchanged graph match within a fraction of a dB.
const TONE_TOLERANCE = 0.02;
const LEVEL_TOLERANCE_DB = 1.5;

let fixture: MusicFixture;
let evidence: Evidence;

test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  evidence = new Evidence(fixture.evidenceDir, info.project.name, {
    browser: info.project.name, browser_version: browser.version(),
    channel: info.project.use.channel, server_version: fixture.serverVersion,
    server_build_revision: fixture.buildRevision, checkout: fixture.checkout, origin: fixture.origin, run_id: fixture.runId,
  });
});

test.beforeEach(async ({ page }) => {
  await page.addInitScript(installAudioTap);
});

/** Run a journey and always leave an evidence record, also on failure. */
async function journey(scenario: string, body: (data: Record<string, unknown>, shots: string[]) => Promise<void>) {
  const data: Record<string, unknown> = {};
  const shots: string[] = [];
  try {
    await body(data, shots);
    evidence.record({ feature: FEATURE, scenario, result: 'pass', data, artifacts: shots });
  } catch (error) {
    evidence.record({ feature: FEATURE, scenario, result: 'fail', detail: String(error).slice(0, 400), data, artifacts: shots });
    throw error;
  }
}

async function shot(page: Page, shots: string[], name: string) {
  const path = evidence.path(`${name}.png`);
  await page.screenshot({ path });
  shots.push(path);
}

test('sign-in refuses a wrong password and keeps typed text intact', async ({ page }) => {
  await journey('sign-in: wrong password refused, typing not intercepted', async (data, shots) => {
    await page.goto('/login.html');
    await expect(page.locator('#username')).toBeFocused();
    // Letters that are global shortcuts elsewhere (j k l p / ?) must land in the field.
    await page.keyboard.type('jklp/? ' + fixture.admin.username);
    await expect(page.locator('#username')).toHaveValue('jklp/? ' + fixture.admin.username);
    await page.locator('#username').fill(fixture.admin.username);
    await page.keyboard.press('Tab');
    await page.keyboard.type('definitely-wrong');
    await page.keyboard.press('Enter');
    await expect(page.locator('#login-error')).not.toBeEmpty();
    data.error_text = await page.locator('#login-error').textContent();
    expect(new URL(page.url()).pathname).toBe('/login.html');
    await shot(page, shots, 'signin-wrong-password');
  });
});

test('keyboard journey: open album, play, help, search, pause (admin, desktop dark)', async ({ page }) => {
  const longRoad = track(fixture, `Music/${ALBUM}/02 - Long Road Home.mp3`);
  await journey('keyboard journey: 1/l/Enter, play, ?/Escape focus restore, / typing, p pause', async (data, shots) => {
    await page.setViewportSize({ width: 1366, height: 860 });
    await keyboardLogin(page, fixture.admin);
    await resetProgress(page, longRoad.id);
    await page.keyboard.press('1');
    await expect(page).toHaveURL(new RegExp(`set=${fixture.setIds.Music}`));
    await selectByKeys(page, ALBUM);
    await page.keyboard.press('Enter');
    await expect(page.getByRole('button', { name: longRoad.file_name, exact: true })).toBeVisible();
    await selectByKeys(page, longRoad.file_name);
    await page.keyboard.press('Enter');
    data.playing = await expectPlaying(page, longRoad.id);
    const out = await captureOutput(page, 2500, evidence.path('keyboard-long-road-home.wav'));
    data.capture = { routed: out.routed, dropouts: out.dropouts, segments: out.analysis.segments };
    expect(out.routed, 'page audio is routed through Web Audio and captured').toBe(true);
    expect(mainSegment(out.analysis)?.tone_hz).toBeCloseTo(longRoad.tone_hz[0], -1);
    shots.push(out.wav);
    await shot(page, shots, 'desktop-dark-playing');

    const before = (await audioState(page)).active;
    await page.keyboard.press('?');
    await expect(page.locator('#help-modal')).toHaveClass(/open/);
    await expect(page.locator('#help-modal')).toContainText('Keyboard Shortcuts');
    await shot(page, shots, 'desktop-dark-help');
    await page.keyboard.press('Escape');
    await expect(page.locator('#help-modal')).not.toHaveClass(/open/);
    data.focus = { before_help: before, after_help: (await audioState(page)).active };
    expect((await audioState(page)).active).toContain(longRoad.file_name);

    await page.keyboard.press('/');
    await expect(page.getByRole('searchbox', { name: 'Search media' })).toBeFocused();
    await page.keyboard.type('Harbour p');
    await expect(page.getByRole('searchbox', { name: 'Search media' })).toHaveValue('Harbour p');
    expect((await audioState(page)).paused, 'typing "p" in search does not pause').toBe(false);
    await shot(page, shots, 'desktop-dark-search');
    await page.keyboard.press('Escape');
    await expect.poll(async () => (await audioState(page)).active).not.toContain('search-input');

    await page.keyboard.press('p');
    await expect.poll(async () => (await audioState(page)).paused).toBe(true);
    await page.keyboard.press('p');
    await expect.poll(async () => (await audioState(page)).paused).toBe(false);
    data.pause_resume = 'p paused and resumed';
  });
});

test('touch/pointer journey: granted set only, card Play (regular user, narrow light)', async ({ browser }, info) => {
  const firstLight = track(fixture, `Music/${ALBUM}/01 - First Light.mp3`);
  const hidden = track(fixture, 'Restricted/Admin Only - Hidden Album/01 - Hidden Track.mp3');
  // Chrome emulates a touch screen: taps arrive with pointerType "touch",
  // which opens Player's card action menu. Playwright cannot synthesise
  // touch input in system Firefox over WebDriver BiDi (taps reach no
  // element), so Firefox runs the same journey with the mouse - the pointer
  // alternative - and the missing touch check is recorded as a gap.
  const touch = info.project.name === 'chrome';
  const press = (locator: Locator) => (touch ? locator.tap() : locator.click());
  const context = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: touch });
  const page = await context.newPage();
  await page.addInitScript(installAudioTap);
  try {
    if (!touch) {
      evidence.record({ feature: FEATURE, scenario: 'touch input in Firefox', result: 'gap', proof: 'none',
        detail: 'Playwright cannot emulate touch in system Firefox (moz-firefox over BiDi); the pointer journey ran with the mouse instead.' });
    }
    await journey('touch or pointer journey: sign-in, theme, sidebar, folder, card Play', async (data, shots) => {
      data.input = touch ? 'touch' : 'mouse';
      await pointerSignIn(page, press);
      await resetProgress(page, firstLight.id);
      await press(page.getByRole('button', { name: 'Toggle theme' }));
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
      await press(page.getByRole('button', { name: 'Toggle sets sidebar' }));
      const sets = page.locator('#set-list [role="button"]');
      await expect(sets.first()).toBeVisible();
      data.visible_sets = await sets.evaluateAll((rows) => rows.map((r) => r.getAttribute('aria-label')));
      expect(data.visible_sets).toEqual(fixture.userSets.map((name) => `Set ${name}`));
      await shot(page, shots, 'narrow-light-sidebar');
      await press(page.locator('#set-list').getByRole('button', { name: 'Set Music', exact: true }));
      await press(page.getByRole('button', { name: `Folder ${ALBUM}` }));
      await playFromCard(page, firstLight.file_name, touch, shots);
      data.playing = await expectPlaying(page, firstLight.id);
      const out = await captureOutput(page, 2500, evidence.path('narrow-first-light.wav'));
      data.capture = { routed: out.routed, segments: out.analysis.segments };
      expect(out.routed).toBe(true);
      expect(mainSegment(out.analysis)?.tone_hz).toBeCloseTo(firstLight.tone_hz[0], -1);
      shots.push(out.wav);
      await shot(page, shots, 'narrow-light-playing');
      data.target_sizes = await targetSizes(page);
      for (const [name, size] of Object.entries(data.target_sizes as Record<string, number[]>)) {
        expect(Math.min(...size), `${name} is at least 24 CSS px (WCAG 2.2 target size)`).toBeGreaterThanOrEqual(24);
      }
      const refused = await page.request.get(`/api/v1/media/${hidden.id}`);
      data.restricted_media_status = refused.status();
      expect([403, 404]).toContain(refused.status());
    });
  } finally {
    await context.close();
  }
});

/** Sign in as the regular user with taps (Chrome) or clicks (Firefox) on the fields. */
async function pointerSignIn(page: Page, press: (locator: Locator) => Promise<void>) {
  await page.goto('/login.html');
  await press(page.locator('#username'));
  await page.keyboard.type(fixture.user.username);
  await press(page.locator('#password'));
  await page.keyboard.type(fixture.user.password);
  await press(page.getByRole('button', { name: 'Sign In' }));
  await page.waitForURL((url) => url.pathname === '/');
}

/**
 * Start a card: a tap opens Player's card action menu, whose Play button
 * plays; with the mouse, hover the card and click its ▶ button.
 */
async function playFromCard(page: Page, fileName: string, touch: boolean, shots: string[]) {
  const card = page.getByRole('button', { name: fileName, exact: true });
  if (!touch) {
    await card.hover();
    await card.locator('button[data-action="play"][title="Play"]').click();
    return;
  }
  await card.tap();
  const menu = page.getByRole('group', { name: 'Media actions' });
  await expect(menu).toBeVisible();
  await shot(page, shots, 'narrow-light-card-menu');
  await menu.getByRole('button', { name: 'Play', exact: true }).tap();
}

/** Width and height of the touch controls used in the narrow journey. */
async function targetSizes(page: Page): Promise<Record<string, number[]>> {
  return page.evaluate(() => {
    const sizes: Record<string, number[]> = {};
    for (const id of ['sidebar-toggle', 'theme-toggle', 'help-toggle', 'btn-play']) {
      const element = document.getElementById(id);
      const box = element?.getBoundingClientRect();
      if (box && box.width) sizes[id] = [Math.round(box.width), Math.round(box.height)];
    }
    return sizes;
  });
}

// Audio output calibration: the capture identifies tones, measures levels,
// bands and the gap at an automatic track change. Each test signs in afresh.

test('audio output capture: ReplayGain fixture levels and tones', async ({ page }) => {
  const loud = track(fixture, 'Music/Gain Check/Loud.flac');
  const quiet = track(fixture, 'Music/Gain Check/Quiet.flac');
  await keyboardLogin(page, fixture.admin);
  await journey('capture calibration: levels of Loud/Quiet differ by 18 dB, tones identified', async (data, shots) => {
    await openFolder(page, fixture.setIds.Music, 'Gain Check');
    const levels: Record<string, number | undefined> = {};
    for (const item of [loud, quiet]) {
      await playCard(page, item.file_name, item.id);
      const out = await captureOutput(page, 2000, evidence.path(`gain-${item.file_name}.wav`));
      const segment = mainSegment(out.analysis);
      levels[item.file_name] = segment?.rms_dbfs;
      expect(segment?.tone_hz ?? 0).toBeGreaterThan(item.tone_hz[0] * (1 - TONE_TOLERANCE));
      expect(segment?.tone_hz ?? 0).toBeLessThan(item.tone_hz[0] * (1 + TONE_TOLERANCE));
      shots.push(out.wav);
    }
    data.rms_dbfs = levels;
    const difference = (levels[loud.file_name] ?? 0) - (levels[quiet.file_name] ?? 0);
    data.measured_difference_db = difference;
    expect(Math.abs(difference - (loud.level_dbfs - quiet.level_dbfs))).toBeLessThan(LEVEL_TOLERANCE_DB);
  });
});

test('audio output capture: EQ probe bands', async ({ page }) => {
  const probe = track(fixture, 'Music/EQ Probe/Three Tones.flac');
  await keyboardLogin(page, fixture.admin);
  await journey('capture calibration: EQ probe bands 100 Hz / 1 kHz / 8 kHz measured (flat today)', async (data, shots) => {
    await openFolder(page, fixture.setIds.Music, 'EQ Probe');
    await playCard(page, probe.file_name, probe.id);
    const out = await captureOutput(page, 2000, evidence.path('eq-probe.wav'), probe.tone_hz);
    shots.push(out.wav);
    const bands = mainSegment(out.analysis)?.bands_dbfs ?? {};
    data.bands_dbfs = bands;
    const values = Object.values(bands);
    expect(values).toHaveLength(3);
    expect(Math.max(...values) - Math.min(...values)).toBeLessThan(LEVEL_TOLERANCE_DB);
  });
});

test('audio output capture: gap at an automatic track change', async ({ page }) => {
  const first = track(fixture, 'Music/Gapless Suite (FLAC)/01 - Movement I.flac');
  const second = track(fixture, 'Music/Gapless Suite (FLAC)/02 - Movement II.flac');
  await keyboardLogin(page, fixture.admin);
  await journey('capture timing: gap at automatic change Movement I -> II measured (baseline)', async (data, shots) => {
    await openFolder(page, fixture.setIds.Music, 'Gapless Suite (FLAC)');
    await resetProgress(page, second.id);
    await playCard(page, first.file_name, first.id);
    // Harness step: jump near the end so the automatic change happens soon.
    await page.evaluate((t) => { (document.getElementById('media-audio') as HTMLAudioElement).currentTime = t; }, first.duration_s - 1.5);
    const out = await captureOutput(page, 5000, evidence.path('gapless-flac-transition.wav'));
    shots.push(out.wav);
    data.after_change = await audioState(page);
    expect((data.after_change as { src: string }).src).toMatch(new RegExp(`/media/${second.id}/`));
    data.segments = out.analysis.segments;
    data.gaps_ms = out.analysis.gaps_ms;
    data.dropouts = out.dropouts;
    // The capture must see audio on both sides of the change; whether there
    // is a gap is today's behaviour, recorded for M21 to improve on.
    expect(out.analysis.segments.length).toBeGreaterThanOrEqual(1);
    expect(out.analysis.audible_s).toBeGreaterThan(2);
  });
});
