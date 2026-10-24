import { expect, test } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { keyboardLogin, openFolder } from './helpers/music-ui';

const FEATURE = 'B9 artwork badge contrast';
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

function srgb(channel: number) {
  const normalized = channel / 255;
  return normalized <= 0.04045
    ? normalized / 12.92
    : ((normalized + 0.055) / 1.055) ** 2.4;
}

function contrast(a: number[], b: number[]) {
  const luminance = (color: number[]) => 0.2126 * srgb(color[0]) + 0.7152 * srgb(color[1]) + 0.0722 * srgb(color[2]);
  const [lighter, darker] = [luminance(a), luminance(b)].sort((left, right) => right - left);
  return (lighter + 0.05) / (darker + 0.05);
}

function rgba(value: string) {
  const components = value.match(/[\d.]+/g)?.map(Number);
  if (!components || components.length < 3) throw new Error(`Cannot parse CSS color ${value}`);
  return { color: components.slice(0, 3), alpha: components[3] ?? 1 };
}

test('B9: duration/size artwork badge remains legible in light and dark themes', async ({ page }) => {
  const data: Record<string, unknown> = {};
  const artifacts: string[] = [];
  try {
    const firstLight = track(fixture, `Music/${ALBUM}/01 - First Light.mp3`);
    await page.setViewportSize({ width: 1366, height: 860 });
    await keyboardLogin(page, fixture.admin);
    await openFolder(page, fixture.setIds.Music, ALBUM);

    const badge = page.locator(`#media-grid .media-card[data-id="${firstLight.id}"] .badge`);
    await expect(badge).toBeVisible();
    await expect(badge).toContainText('00:12');

    const settings = page.getByRole('button', { name: 'Settings', exact: true });
    const mode = page.locator('#appearance-mode');
    for (const theme of ['light', 'dark']) {
      await settings.click();
      await mode.selectOption(theme);
      await page.keyboard.press('Escape');
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme);

      const colors = await badge.evaluate((element) => {
        const style = getComputedStyle(element);
        return { background: style.backgroundColor, foreground: style.color };
      });
      const background = rgba(colors.background);
      const foreground = rgba(colors.foreground).color;
      // The semi-transparent scrim is least bright over black artwork.  This
      // is therefore the conservative rendered contrast, independent of the
      // particular cover image behind the chip.
      const composited = background.color.map((channel) => channel * background.alpha);
      const ratio = contrast(composited, foreground);
      expect(ratio, `${theme} artwork badge contrast`).toBeGreaterThanOrEqual(4.5);
      data[theme] = { ...colors, contrast_over_black_artwork: ratio };

      const shot = evidence.path(`b9-${theme}-artwork-badge.png`);
      await page.screenshot({ path: shot });
      artifacts.push(shot);
    }
    evidence.record({ feature: FEATURE, scenario: 'actual duration/size badge in light and dark themes', result: 'pass', data, artifacts });
  } catch (error) {
    evidence.record({ feature: FEATURE, scenario: 'actual duration/size badge in light and dark themes', result: 'fail', detail: String(error).slice(0, 400), data, artifacts });
    throw error;
  }
});
