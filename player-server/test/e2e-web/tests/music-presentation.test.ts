import { expect, test } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { audioState, expectPlaying, keyboardLogin, openFolder, playCard } from './helpers/music-ui';

const FEATURE = 'M68 appearance presentation';
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

test('M68: appearance choices persist during local playback, including offline changes', async ({ page }) => {
  const data: Record<string, unknown> = {};
  const artifacts: string[] = [];
  try {
    const firstLight = track(fixture, `Music/${ALBUM}/01 - First Light.mp3`);
    await page.setViewportSize({ width: 1366, height: 860 });
    await keyboardLogin(page, fixture.admin);
    await openFolder(page, fixture.setIds.Music, ALBUM);
    await playCard(page, firstLight.title, firstLight.id);
    await expectPlaying(page, firstLight.id);

    const visuals = page.getByLabel('Audio visualization');
    await expect(visuals).toBeVisible();
    await visuals.selectOption('off');
    const cover = page.locator('#cover-art');
    // This synthetic track has no per-track artwork thumbnail. Turning
    // visuals off must therefore retain its honest missing-art fallback,
    // rather than showing stale art from a previous source.
    await expect(cover).toBeHidden();

    // The settings rows must also stay usable beside the compact player on a
    // phone-sized viewport.
    await page.setViewportSize({ width: 390, height: 844 });

    const settings = page.getByRole('button', { name: 'Settings', exact: true });
    await settings.click();
    const dialog = page.locator('#settings-modal');
    await expect(dialog).toHaveClass(/open/);

    const mode = page.locator('#appearance-mode');
    const accent = page.locator('#appearance-accent');
    const shape = page.locator('#appearance-shape');
    const fit = page.locator('#appearance-fit');
    const observed: Record<string, string> = {};
    for (const theme of ['dark', 'light']) {
      await mode.selectOption(theme);
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
      for (const choice of ['blue', 'teal', 'plum']) {
        await accent.selectOption(choice);
        await expect(page.locator('html')).toHaveAttribute('data-accent', choice);
        observed[`${theme}-${choice}`] = await page.locator('html').evaluate((root) =>
          getComputedStyle(root).getPropertyValue('--accent').trim());
        const shot = evidence.path(`m68-${theme}-${choice}.png`);
        await page.screenshot({ path: shot });
        artifacts.push(shot);
      }
    }
    expect(Object.values(observed).every(Boolean)).toBe(true);

    await shape.selectOption('square');
    await expect(page.locator('html')).toHaveAttribute('data-shape', 'square');
    await expect(cover).toHaveCSS('border-radius', '0px');
    await shape.selectOption('circle');
    await expect(page.locator('html')).toHaveAttribute('data-shape', 'circle');
    await expect(cover).toHaveCSS('clip-path', /circle/);
    await shape.selectOption('rounded');
    await expect(cover).toHaveCSS('border-radius', '14px');
    await fit.selectOption('cover');
    await expect(cover).toHaveCSS('object-fit', 'cover');
    await fit.selectOption('contain');
    await expect(cover).toHaveCSS('object-fit', 'contain');

    await page.keyboard.press('Escape');
    await expect(dialog).not.toHaveClass(/open/);
    await expect(settings).toBeFocused();
    await expect.poll(async () => (await audioState(page)).paused).toBe(false);

    await page.reload({ waitUntil: 'domcontentloaded' });
    await settings.click();
    await expect(mode).toHaveValue('light');
    await expect(accent).toHaveValue('plum');
    await expect(shape).toHaveValue('rounded');
    await expect(fit).toHaveValue('contain');

    await page.context().setOffline(true);
    await accent.selectOption('teal');
    await expect(page.locator('html')).toHaveAttribute('data-accent', 'teal');
    data.offline_saved_accent = await page.evaluate(() => localStorage.getItem('kiss-art-accent'));
    expect(data.offline_saved_accent).toBe('teal');
    await page.context().setOffline(false);
    await page.keyboard.press('Escape');
    data.accents = observed;
    data.focus_restored = true;
    data.playing_before_reload = true;
    evidence.record({ feature: FEATURE, scenario: 'narrow settings: all themes/accents, artwork display, restart and offline persistence', result: 'pass', data, artifacts });
  } catch (error) {
    evidence.record({ feature: FEATURE, scenario: 'narrow settings: all themes/accents, artwork display, restart and offline persistence', result: 'fail', detail: String(error).slice(0, 400), data, artifacts });
    throw error;
  }
});
