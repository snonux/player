import { expect, test } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import { loadFixture, track } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, openFolder, playCard, expectPlaying, captureOutput, assertValidCapture, mainSegment } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires a disposable local music fixture');
test.skip(!process.env.PLAYER_M53_ALTERNATE, 'Requires a separately hosted alias of the same disposable server');

test('M53 browser page origin retains its account and audible playback', async ({ page, context, browser }, info) => {
  const fixture = loadFixture();
  const directory = `${process.env.PLAYER_M53_EVIDENCE || fixture.evidenceDir}/m53-${info.project.name}`;
  mkdirSync(directory, { recursive: true });
  const requests: { origin: string; path: string }[] = [];
  page.on('request', request => {
    const url = new URL(request.url());
    if (url.pathname.startsWith('/api/')) requests.push({ origin: url.origin, path: url.pathname });
  });
  await page.addInitScript(installAudioTap);
  await keyboardLogin(page, fixture.user);
  const identity = await page.request.get('/api/v1/server-identity');
  expect(identity.status()).toBe(200);
  expect(identity.headers()['cache-control']).toBe('no-store');
  const config = await (await page.request.get('/api/v1/config')).json();
  const song = track(fixture, 'Music/EQ Probe/Three Tones.flac');
  await openFolder(page, fixture.setIds.Music, 'EQ Probe');
  await playCard(page, song.title, song.id);
  await expectPlaying(page, song.id);
  const output = await captureOutput(page, 1500, `${directory}/browser.wav`, [100, 1000, 8000]);
  assertValidCapture(output);
  expect(mainSegment(output.analysis)!.duration_s).toBeGreaterThan(1);
  await page.keyboard.press('p');
  await expect.poll(() => page.locator('#media-audio').evaluate((audio: HTMLAudioElement) => audio.paused)).toBe(true);
  await page.keyboard.press('p');
  await expectPlaying(page, song.id);
  // The alternate address is another origin and does not inherit the account.
  const alternate = await context.newPage();
  await alternate.goto(process.env.PLAYER_M53_ALTERNATE!);
  await expect(alternate.locator('#username')).toBeVisible();
  await alternate.close();
  expect(requests.every(request => request.origin === fixture.origin)).toBe(true);
  expect((await (await page.request.get('/api/v1/config')).json()).user_id).toBe(config.user_id);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: `${directory}/narrow-dark.png`, fullPage: true });
  await page.getByRole('button', { name: 'Toggle theme', exact: true }).click();
  await page.screenshot({ path: `${directory}/narrow-light.png`, fullPage: true });
  await page.keyboard.press('/');
  await expect(page.locator('#search-input')).toBeFocused();
  await page.keyboard.type('Harbour');
  await expect(page.locator('#search-input')).toHaveValue('Harbour');
  await page.keyboard.press('Escape');
  await context.setOffline(true);
  try {
    await page.keyboard.press('/');
    await page.locator('#search-input').fill('Different');
    await page.keyboard.press('Enter');
    await expect(page.getByRole('button', { name: 'Retry', exact: true })).toBeVisible();
    await page.screenshot({ path: `${directory}/offline.png`, fullPage: true });
  } finally {
    await context.setOffline(false);
  }
  await page.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(page.locator('[data-search-kind=song]')).toHaveCount(1);
  writeFileSync(`${directory}/result.json`, JSON.stringify({ browser: browser.version(), server: fixture.buildRevision, accountId: config.user_id, requests, audio: output.analysis }, null, 2));
});
