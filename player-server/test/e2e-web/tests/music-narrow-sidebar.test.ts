import { expect, test } from '@playwright/test';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { keyboardLogin } from './helpers/music-ui';

const FEATURE = 'B10 narrow sidebar navigation';
const ALBUM = 'Aurora Lane - Night Transit (2021)';
test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'PLAYER_MUSIC_FIXTURE is not set');
let fixture: MusicFixture;
let evidence: Evidence;

test.beforeAll(async ({ browser }, info) => {
  fixture = loadFixture();
  evidence = new Evidence(fixture.evidenceDir, info.project.name, { browser: info.project.name, browser_version: browser.version(),
    channel: info.project.use.channel, server_version: fixture.serverVersion, checkout: fixture.checkout, run_id: fixture.runId });
});

test('B10: set and folder navigation dismiss the narrow sidebar and move focus to content', async ({ page }) => {
  const artifacts: string[] = [];
  try {
    await page.setViewportSize({ width: 390, height: 844 });
    await keyboardLogin(page, fixture.admin);
    const sidebar = page.locator('#sidebar');
    const toggle = page.getByRole('button', { name: 'Toggle sets sidebar', exact: true });
    await toggle.click();
    await expect(sidebar).toHaveClass(/open/);
    const music = page.locator('#set-list .set-row[aria-label="Set Music"]');
    await music.click();
    await expect(sidebar).not.toHaveClass(/open/);
    const folder = page.getByRole('button', { name: `Folder ${ALBUM}`, exact: true });
    await expect(folder).toBeVisible();
    await expect(folder).toBeFocused();

    // Keyboard activation follows the same narrow navigation/focus path.
    await toggle.click();
    await music.focus();
    await page.keyboard.press('Enter');
    await expect(sidebar).not.toHaveClass(/open/);
    await expect(folder).toBeFocused();
    await folder.click();
    await expect(sidebar).not.toHaveClass(/open/);
    await expect(page.locator('#media-grid .media-card').first()).toBeVisible();
    expect(await page.evaluate(() => !document.getElementById('sidebar')?.contains(document.activeElement))).toBe(true);
    const shot = evidence.path('b10-narrow-folder-content.png');
    await page.screenshot({ path: shot });
    artifacts.push(shot);
    evidence.record({ feature: FEATURE, scenario: 'pointer and keyboard set/folder navigation', result: 'pass', artifacts });
  } catch (error) {
    evidence.record({ feature: FEATURE, scenario: 'pointer and keyboard set/folder navigation', result: 'fail', detail: String(error).slice(0, 400), artifacts });
    throw error;
  }
});
