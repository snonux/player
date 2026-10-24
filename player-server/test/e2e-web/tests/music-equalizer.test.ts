import { expect, test } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { loadFixture, track } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { assertValidCapture, audioState, captureOutput, keyboardLogin, mainSegment, openFolder, playCard } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Disposable music fixture required');

test('M25: real output follows bands, presets, bypass and reset', async ({ page, browser }, info) => {
  const fixture = loadFixture();
  const dir = join(process.env.PLAYER_EQ_EVIDENCE || fixture.evidenceDir, info.project.name);
  mkdirSync(dir, { recursive: true });
  await page.addInitScript(installAudioTap);
  await keyboardLogin(page, fixture.user);
  await openFolder(page, fixture.setIds.Music, 'EQ Probe');
  const probe = track(fixture, 'Music/EQ Probe/Three Tones.flac');
  await playCard(page, probe.title, probe.id);
  const panel = page.locator('.equalizer-control');
  await panel.locator('summary').click();
  const preset = page.getByLabel('Equalizer preset', { exact: true });
  const enabled = page.getByLabel('Enable equalizer', { exact: true });
  const reset = panel.getByRole('button', { name: 'Reset flat', exact: true });
  const bands: Record<string, Record<string, number>> = {};
  async function capture(name: string) {
    expect(await page.evaluate(async () => {
      const path = '/js/playback.js';
      return (await import(path)).currentMediaId();
    })).toBe(probe.id);
    const timeline = page.locator('#progress-track');
    const bounds = (await timeline.boundingBox())!;
    await timeline.click({ position: { x: bounds.width * .02, y: bounds.height / 2 } });
    await expect.poll(async () => (await audioState(page)).time).toBeLessThan(1);
    await expect.poll(async () => (await audioState(page)).paused).toBe(false);
    const output = await captureOutput(page, 1500, join(dir, name + '.wav'), [100, 1000, 8000]);
    assertValidCapture(output);
    writeFileSync(join(dir, name + '.json'), JSON.stringify(output.analysis, null, 2));
    const segment = mainSegment(output.analysis)!;
    expect(segment.duration_s).toBeGreaterThan(1);
    bands[name] = Object.fromEntries(Object.entries(segment.bands_dbfs!).map(([f, db]) => [String(Number(f)), db]));
    return bands[name];
  }
  function closeTo(name: string, reference: string, expected: number[]) {
    for (const [i, frequency] of ['100', '1000', '8000'].entries()) {
      expect(Math.abs(bands[name][frequency] - bands[reference][frequency] - expected[i]),
        `${name} ${frequency} Hz change`).toBeLessThan(1.5);
    }
  }
  await reset.click();
  await capture('flat');
  expect(Math.max(...Object.values(bands.flat)) - Math.min(...Object.values(bands.flat))).toBeLessThan(1.5);
  // Native range keys must adjust the curve without triggering page navigation.
  const middle = page.getByLabel('Equalizer 910 Hz', { exact: true });
  await middle.focus();
  await middle.press('Home');
  await expect(middle).toHaveValue('-12');
  await expect(preset).toHaveValue('Custom');
  await capture('mid-cut');
  // Five peaking filters, Q=.7: the isolated 910 Hz -12 dB band has this
  // independently calculated response at the fixture's three frequencies.
  closeTo('mid-cut', 'flat', [-.35, -11.69, -.23]);
  await enabled.uncheck();
  await expect(middle).toHaveValue('-12');
  await capture('bypass');
  closeTo('bypass', 'flat', [0, 0, 0]);
  await enabled.check();
  await preset.selectOption('Bass boost');
  await capture('bass');
  // Headroom-reserved bass curve must favor bass over voice/high frequencies.
  expect((bands.bass['100'] - bands.flat['100']) - (bands.bass['1000'] - bands.flat['1000'])).toBeGreaterThan(3);
  expect((bands.bass['100'] - bands.flat['100']) - (bands.bass['8000'] - bands.flat['8000'])).toBeGreaterThan(3);
  await preset.selectOption('Voice');
  await capture('voice');
  expect((bands.voice['1000'] - bands.flat['1000']) - (bands.voice['100'] - bands.flat['100'])).toBeGreaterThan(3);
  // The already-buffered source keeps playing; local DSP needs no server.
  await page.context().setOffline(true);
  await preset.selectOption('Bass boost');
  await capture('offline-bass');
  closeTo('offline-bass', 'bass', [0, 0, 0]);
  await page.context().setOffline(false);
  await reset.click();
  await capture('reset');
  closeTo('reset', 'flat', [0, 0, 0]);
  await page.getByRole('button', { name: 'Play/Pause (Space/p)', exact: true }).click();
  await expect.poll(async () => (await audioState(page)).paused).toBe(true);
  await expect(enabled).not.toBeChecked();
  await preset.focus();
  await preset.press('Escape');
  await expect(panel.locator('summary')).toBeFocused();
  await expect(panel.locator('details')).not.toHaveAttribute('open');
  await panel.locator('summary').press('Enter');
  await preset.selectOption('Voice');
  await page.reload();
  await openFolder(page, fixture.setIds.Music, 'EQ Probe');
  await playCard(page, probe.title, probe.id);
  await panel.locator('summary').click();
  await expect(preset).toHaveValue('Voice');
  await expect(enabled).toBeChecked();
  await capture('voice-after-reload');
  closeTo('voice-after-reload', 'voice', [0, 0, 0]);
  await reset.click();
  await page.getByRole('button', { name: 'Play/Pause (Space/p)', exact: true }).click();
  await expect.poll(async () => (await audioState(page)).paused).toBe(true);
  for (const [theme, width] of [['light', 390], ['dark', 1280]] as const) {
    await page.setViewportSize({ width, height: 1000 });
    await page.evaluate(theme => document.documentElement.setAttribute('data-theme', theme), theme);
    if (width < 600) await page.getByRole('button', { name: 'More player controls', exact: true }).click();
    await panel.screenshot({ path: join(dir, theme + '.png') });
  }
  writeFileSync(join(dir, 'results.json'), JSON.stringify({ browser: browser.version(), bands, source: probe.path,
    checkout: fixture.checkout, server: fixture.buildRevision }, null, 2));
});
