/** M61: installed-browser internet-radio journey against the local live stream. */
import { test, expect, type Page } from '@playwright/test';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, captureOutput, assertValidCapture, mainSegment } from './helpers/music-ui';

test.skip(!process.env.PLAYER_M61_ISOLATED, 'Requires disposable M61 fixture');
test.use({ serviceWorkers: 'block' });
let fixture: MusicFixture, evidence: Evidence;
async function openRadio(page: Page) {
  const shortcut = page.getByRole('button', { name: 'Internet radio', exact: true });
  if (!await shortcut.isVisible()) await page.getByRole('button', { name: 'Toggle sets sidebar' }).click();
  await shortcut.click();
  await expect(page.locator('#radio-modal')).toHaveClass(/open/);
  await expect.poll(() => page.locator('#radio-modal').evaluate(el => getComputedStyle(el).opacity)).toBe('1');
}
test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  evidence = new Evidence(process.env.PLAYER_M61_EVIDENCE!, info.project.name,
    { feature: 'M61', browser_version: browser.version(), server_version: fixture.serverVersion });
});

test('M61 station editor, permissions, live output, reconnect and keyboard', async ({ browser }) => {
  test.setTimeout(180_000);
  const admin = await browser.newPage();
  const viewer = await browser.newPage();
  await viewer.addInitScript(installAudioTap);
  const stream = `${fixture.doublesOrigin}/radio/fixture.mp3`;
  const down = `${fixture.doublesOrigin}/radio/down.mp3`;
  const artwork = process.env.PLAYER_M61_ART_URL || '';
  if (!fixture.doublesOrigin || !artwork) throw new Error('M61 requires fixture doubles and PLAYER_M61_ART_URL');
  try {
    await admin.bringToFront();
    await keyboardLogin(admin, fixture.admin);
    await openRadio(admin);
    const modal = admin.locator('#radio-modal');
    await expect(modal).toHaveClass(/open/);
    await expect(modal).toContainText('No stations yet');
    await modal.getByRole('button', { name: 'Add station' }).click();
    const editor = admin.locator('#radio-editor');
    await editor.getByLabel('Station name').fill('Fixture Radio');
    await editor.getByLabel('Stream URL').fill(stream);
    await editor.getByLabel('Website URL (optional)').fill('https://example.org/radio');
    await editor.getByLabel('Artwork URL (optional)').fill(artwork);
    await editor.getByLabel('Allow local-network station').check();
    await editor.getByRole('button', { name: 'Save' }).click();
    await expect(editor).not.toHaveClass(/open/);
    await expect(modal.locator('.radio-station')).toHaveCount(1);
    const station = modal.locator('.radio-station').first();
    await modal.getByRole('searchbox', { name: 'Search stations' }).focus();
    await admin.keyboard.type('missing');
    await expect(modal).toContainText('No matching stations');
    await modal.getByRole('searchbox', { name: 'Search stations' }).fill('Fixture');
    await station.getByRole('button', { name: 'Edit' }).click();
    await editor.getByLabel('Station name').fill('Fixture Radio Live');
    await editor.getByRole('button', { name: 'Save' }).click();
    await expect(station).toContainText('Fixture Radio Live');
    await expect.poll(() => station.locator('img').evaluate((img: HTMLImageElement) => img.naturalWidth)).toBeGreaterThan(0);
    const response = await admin.request.get(`/api/radio/${await station.getAttribute('data-station')}/artwork`);
    expect(response.ok()).toBe(true);
    expect(response.headers()['content-type']).toContain('image/jpeg');
    await station.getByRole('button', { name: 'Edit' }).click();
    await editor.getByLabel('Stream URL').fill('https://example.org/live#fragment');
    await editor.getByRole('button', { name: 'Save' }).click();
    await expect(editor.getByRole('alert')).toContainText('without credentials or fragments');
    await editor.getByLabel('Stream URL').fill(stream);
    await editor.getByRole('button', { name: 'Save' }).click();
    await expect(editor).not.toHaveClass(/open/);

    await viewer.bringToFront();
    await keyboardLogin(viewer, fixture.user);
    await openRadio(viewer);
    const viewerModal = viewer.locator('#radio-modal');
    const viewerStation = viewerModal.locator('.radio-station').first();
    await expect(viewerStation).toContainText('Fixture Radio Live');
    await expect(viewerModal.getByRole('button', { name: 'Add station' })).toBeHidden();
    await expect(viewerStation.getByRole('button', { name: 'Edit' })).toHaveCount(0);
    expect((await viewer.request.post('/api/radio', { data: { name: 'Forbidden', stream_url: stream } })).status()).toBe(403);
    await viewerModal.getByRole('searchbox', { name: 'Search stations' }).focus();
    await viewer.keyboard.press('Escape');
    await expect(viewerModal).not.toHaveClass(/open/);
    await openRadio(viewer);
    await viewerStation.getByRole('button', { name: 'Play' }).focus();
    await viewer.keyboard.press('p');
    await expect(viewerStation.locator('.radio-state')).toHaveText('Live', { timeout: 25_000 });
    await expect.poll(() => viewer.evaluate(() => {
      const audio = document.querySelector('#media-audio') as HTMLAudioElement;
      return !audio.paused && audio.currentSrc.includes('/api/radio/');
    }), { timeout: 20_000 }).toBe(true);
    expect(await viewer.locator('#player').evaluate(el => el.classList.contains('live-radio'))).toBe(true);
    await expect(viewer.locator('#player .seek-controls')).toBeHidden();
    const sound = await captureOutput(viewer, 1700, evidence.path('live-440hz.wav'));
    assertValidCapture(sound);
    expect(Math.abs(mainSegment(sound.analysis)!.tone_hz! - 440)).toBeLessThan(5);
    await expect(viewerStation.getByRole('button', { name: 'Stop' })).toBeVisible();
    await viewerStation.getByRole('button', { name: 'Stop' }).click();
    await expect(viewerStation.getByRole('button', { name: 'Play' })).toBeVisible();
    await viewerStation.getByRole('button', { name: 'Play' }).click();
    await expect(viewerStation.getByRole('button', { name: 'Stop' })).toBeVisible({ timeout: 20_000 });

    await station.getByRole('button', { name: 'Edit' }).click();
    await editor.getByLabel('Stream URL').fill(down);
    await editor.getByRole('button', { name: 'Save' }).click();
    await viewerModal.getByRole('button', { name: 'Refresh' }).click();
    await viewerModal.locator('.radio-station').first().getByRole('button', { name: 'Stop' }).click();
    await viewerModal.locator('.radio-station').first().getByRole('button', { name: 'Play' }).click();
    await expect(viewerModal.locator('.radio-station').first()).toContainText('Retry', { timeout: 20_000 });
    await station.getByRole('button', { name: 'Edit' }).click();
    await editor.getByLabel('Stream URL').fill(stream);
    await editor.getByRole('button', { name: 'Save' }).click();
    await viewerModal.getByRole('button', { name: 'Refresh' }).click();
    await viewerModal.locator('.radio-station').first().getByRole('button', { name: 'Retry' }).click();
    await expect(viewerModal.locator('.radio-station').first().locator('.radio-state')).toHaveText('Live', { timeout: 20_000 });
    await viewer.context().setOffline(true);
    await viewerModal.getByRole('button', { name: 'Refresh' }).click();
    await expect(viewerModal.locator('#radio-status')).toContainText('Could not reach stations');
    await viewer.context().setOffline(false);
    await viewerModal.getByRole('button', { name: 'Refresh' }).click();
    await expect(viewerModal.locator('#radio-status')).toContainText('Live stations');
    const reconnect = viewerModal.locator('.radio-station').first().getByRole('button', { name: /^(Retry|Play)$/ });
    if (await reconnect.isVisible()) await reconnect.click();
    await expect(viewerModal.locator('.radio-station').first().locator('.radio-state')).toHaveText('Live', { timeout: 20_000 });
    await viewer.setViewportSize({ width: 390, height: 844 });
    expect(await viewer.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await viewer.screenshot({ path: evidence.path('radio-narrow-dark.png'), fullPage: true });
    await expect(viewerModal.locator('.radio-station').first()).toBeVisible();
    await viewerModal.getByRole('button', { name: 'Close internet radio' }).click();
    await viewer.getByRole('button', { name: 'Toggle theme' }).click();
    await openRadio(viewer);
    await viewer.screenshot({ path: evidence.path('radio-narrow-light.png'), fullPage: true });
    await expect(viewerModal.locator('.radio-station').first()).toBeVisible();
    evidence.record({ feature: 'M61', scenario: 'station CRUD, artwork, URL validation, viewer permissions, keyboard, live MP3 output, outage, offline refresh and reconnect', result: 'pass', proof: 'service-double', artifacts: ['live-440hz.wav', 'radio-narrow-dark.png', 'radio-narrow-light.png'] });
  } finally {
    const response = await admin.request.get('/api/radio').catch(() => null);
    if (response?.ok()) for (const station of await response.json()) {
      if (station.name.startsWith('Fixture Radio'))
        await admin.request.delete(`/api/radio/${station.id}`).catch(() => {});
    }
    await admin.close();
    await viewer.close();
  }
});
