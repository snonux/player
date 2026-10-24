import { expect, test } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { keyboardLogin, openFolder } from './helpers/music-ui';

const FEATURE = 'yn3 media card accessibility';
const ALBUM = 'Aurora Lane - Night Transit (2021)';
test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'PLAYER_MUSIC_FIXTURE is not set');
let fixture: MusicFixture;
let evidence: Evidence;
test.beforeAll(async ({ browser }, info) => { fixture = loadFixture(); evidence = new Evidence(fixture.evidenceDir, info.project.name, { browser: info.project.name, browser_version: browser.version(), channel: info.project.use.channel, server_version: fixture.serverVersion, checkout: fixture.checkout, run_id: fixture.runId }); });

test('yn3: a media group exposes its title and separate card actions', async ({ page }) => {
  const artifacts: string[] = [];
  try {
    const item = track(fixture, `Music/${ALBUM}/01 - First Light.mp3`);
    await keyboardLogin(page, fixture.admin);
    await openFolder(page, fixture.setIds.Music, ALBUM);
    const card = page.locator(`#media-grid .media-card[data-id="${item.id}"]`);
    await expect(card).toHaveAttribute('role', 'group');
    const open = card.getByRole('button', { name: item.title, exact: true });
    await expect(open).toBeVisible();
    await open.focus();
    await expect(open).toBeFocused();
    await card.hover();
    for (const name of ['Play', 'Favorite', 'Notes', 'Move to trash', 'Mark as finished', 'Download', 'Tags']) {
      await expect(card.getByRole('button', { name, exact: true })).toBeVisible();
    }
    const screenshot = evidence.path('yn3-card-actions.png');
    await page.screenshot({ path: screenshot });
    artifacts.push(screenshot);
    evidence.record({ feature: FEATURE, scenario: 'title and all visible actions have separate accessible buttons', result: 'pass', artifacts });
  } catch (error) {
    evidence.record({ feature: FEATURE, scenario: 'title and all visible actions have separate accessible buttons', result: 'fail', detail: String(error).slice(0, 400), artifacts });
    throw error;
  }
});
