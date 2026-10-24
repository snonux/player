import { expect, test } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { audioState, keyboardLogin, openFolder, playCard } from './helpers/music-ui';

const FEATURE = 'M23 web playback speed';
const ALBUM = 'Aurora Lane - Night Transit (2021)';
test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'PLAYER_MUSIC_FIXTURE is not set');
let fixture: MusicFixture;
let evidence: Evidence;
test.beforeAll(async ({ browser }, info) => { fixture = loadFixture(); evidence = new Evidence(fixture.evidenceDir, info.project.name, { browser: info.project.name, browser_version: browser.version(), channel: info.project.use.channel, server_version: fixture.serverVersion, checkout: fixture.checkout, run_id: fixture.runId }); });

test('M23: visible speed choices change real audio progression and persist', async ({ page }) => {
  const data: Record<string, unknown> = {};
  const artifacts: string[] = [];
  try {
    const item = track(fixture, `Music/${ALBUM}/02 - Long Road Home.mp3`);
    await keyboardLogin(page, fixture.admin);
    await openFolder(page, fixture.setIds.Music, ALBUM);
    await playCard(page, item.title, item.id);
    const speed = page.getByLabel('Playback speed', { exact: true });
    await expect(speed).toBeVisible();
    await speed.selectOption('2');
    await expect(speed).toHaveValue('2');
    const start = (await audioState(page)).time;
    await page.waitForTimeout(1_000);
    const advanced = (await audioState(page)).time - start;
    expect(advanced, '2× playback advances about two media seconds per wall second').toBeGreaterThan(1.5);
    await page.keyboard.press('<');
    await expect(speed).toHaveValue('1.5');
    await speed.selectOption('1');
    await expect(page.locator('#media-audio')).toHaveJSProperty('playbackRate', 1);
    await page.reload({ waitUntil: 'domcontentloaded' });
    await expect.poll(() => page.evaluate(() => localStorage.getItem('player-audio-speed'))).toBe('1');
    data.media_seconds_at_2x = advanced;
    const shot = evidence.path('m23-speed-control.png');
    await page.screenshot({ path: shot }); artifacts.push(shot);
    evidence.record({ feature: FEATURE, scenario: '2× progression, keyboard step and normal reset persistence', result: 'pass', data, artifacts });
  } catch (error) {
    evidence.record({ feature: FEATURE, scenario: '2× progression, keyboard step and normal reset persistence', result: 'fail', detail: String(error).slice(0, 400), data, artifacts });
    throw error;
  }
});

test('M23: all speed choices, popup handoff and public share stay coherent', async ({ page, browser }) => {
  const item = track(fixture, `Music/${ALBUM}/02 - Long Road Home.mp3`);
  await keyboardLogin(page, fixture.admin);
  await openFolder(page, fixture.setIds.Music, ALBUM);
  await playCard(page, item.title, item.id);
  await page.locator('#btn-play').click();
  const speed = page.getByLabel('Playback speed', { exact: true });
  for (const rate of ['0.5', '1', '1.25', '1.5', '2']) {
    await speed.selectOption(rate);
    await expect(page.locator('#media-audio')).toHaveJSProperty('playbackRate', Number(rate));
  }
  await speed.press('Escape');
  await page.locator('body').click({ position: { x: 5, y: 5 } });
  const opened = page.waitForEvent('popup');
  await page.keyboard.press('d');
  const popup = await opened;
  const popupSpeed = popup.getByLabel('Playback speed', { exact: true });
  await expect(popupSpeed).toHaveValue('2');
  await expect(popup.locator('#media-audio')).toHaveJSProperty('playbackRate', 2);
  await popupSpeed.selectOption('1.25');
  await expect(popup.locator('#media-audio')).toHaveJSProperty('playbackRate', 1.25);
  // Run the same beforeunload handoff as closing the user's popup window.
  await popup.close({ runBeforeUnload: true });
  await expect(page.locator('#detached-bar')).toBeHidden();
  await expect(speed).toHaveValue('1.25');
  await expect(page.locator('#media-audio')).toHaveJSProperty('playbackRate', 1.25);
  await page.getByRole('button', { name: 'Restore player', exact: true }).click();
  await speed.selectOption('1');
  await page.setViewportSize({ width: 390, height: 720 });
  const more = page.getByRole('button', { name: 'More player controls', exact: true });
  await more.click();
  await speed.focus();
  await speed.press('Escape');
  await expect(more).toBeFocused();
  const artifacts: string[] = [];
  for (const theme of ['dark', 'light']) {
    if (await page.locator('html').getAttribute('data-theme') !== theme) {
      await page.getByRole('button', { name: 'Toggle theme', exact: true }).click();
    }
    await more.click();
    await expect(speed).toBeVisible();
    const shot = evidence.path(`m23-narrow-${theme}.png`);
    await page.screenshot({ path: shot }); artifacts.push(shot);
    await speed.selectOption('1');
  }

  const response = await page.request.post(`/api/media/${item.id}/shares`, { data: {} });
  expect(response.ok()).toBeTruthy();
  const share = await response.json();
  const anonymous = await browser.newContext();
  try {
    const shared = await anonymous.newPage();
    await shared.goto(`${fixture.origin}/s/${share.token}`);
    await expect(shared.locator('body')).toHaveAttribute('data-share-viewing', 'open');
    const sharedSpeed = shared.getByLabel('Playback speed', { exact: true });
    await sharedSpeed.selectOption('1.5');
    await expect(shared.locator('#media-audio')).toHaveJSProperty('playbackRate', 1.5);
    await sharedSpeed.selectOption('1');
    evidence.record({ feature: FEATURE, scenario: 'all five rates, detached speed handoff, narrow theme/focus and anonymous share reset', result: 'pass', artifacts });
  } finally {
    await anonymous.close();
    await page.request.delete(`/api/shares/${share.id}`);
  }
});
