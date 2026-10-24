import { expect, test } from '@playwright/test';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { loadFixture } from './helpers/music-fixture';
import { audioState, expectPlaying, keyboardLogin, openFolder, playCard } from './helpers/music-ui';

test.skip(!process.env.PLAYER_LYRIC_FORMAT_CASES, 'Requires additive A03a fixture manifest');
test('A03a formats keep timing, focus, fallback and readable narrow presentation', async ({ page, browser }, info) => {
  test.setTimeout(180_000);
  const fixture = loadFixture();
  const tracks = JSON.parse(readFileSync(process.env.PLAYER_LYRIC_FORMAT_CASES!, 'utf8'));
  const root = process.env.PLAYER_LYRIC_FORMAT_EVIDENCE || `${fixture.evidenceDir}/lyric-formats`;
  const dir = `${root}/${info.project.name}`;
  mkdirSync(dir, { recursive: true });
  const results: unknown[] = [];
  await page.setViewportSize({ width: 390, height: 844 });
  await keyboardLogin(page, fixture.user);
  for (const title of ['TTML', 'YAML', 'ELRC', 'SRT', 'Malformed Fallback', 'Priority', 'YAML Plain', 'Unavailable']) {
    const track = tracks.find((item: any) => item.file_name === `${title}.mp3`);
    await openFolder(page, fixture.setIds.Music, 'A03a Formats');
    await playCard(page, title, track.id);
    await expectPlaying(page, track.id);
    const panel = page.getByRole('region', { name: 'Lyrics' });
    if (title === 'Unavailable') {
      await expect(panel).toBeHidden();
    } else if (title === 'YAML Plain') {
      await expect(panel).toContainText('Café plain fallback');
      await expect(panel.getByRole('button')).toHaveCount(0);
    } else {
      const middle = panel.getByRole('button', { name: /Seek to 0:02: Every mile a quiet song/ });
      const last = panel.getByRole('button', { name: /Seek to 0:08: Harbour lights/ });
      await middle.focus();
      await page.keyboard.press('Enter');
      await expect.poll(async () => (await audioState(page)).time).toBeGreaterThanOrEqual(2);
      expect((await audioState(page)).time).toBeLessThan(4);
      await expect(middle).toBeFocused();
      await expect(middle).toHaveAttribute('aria-current', 'true');
      await page.keyboard.press('p');
      await expect.poll(async () => (await audioState(page)).paused).toBe(true);
      await last.click();
      await expect.poll(async () => (await audioState(page)).time).toBeCloseTo(8, 1);
      await expect(last).toHaveAttribute('aria-current', 'true');
      expect((await audioState(page)).paused).toBe(true);
      await page.context().setOffline(true);
      await middle.click();
      await expect.poll(async () => (await audioState(page)).time).toBeCloseTo(2, 1);
      await page.context().setOffline(false);
      await expect(panel).not.toContainText(/wrong priority|wrong text|<p|start_ms|-->/);
    }
    for (const theme of ['light', 'dark']) {
      await page.getByRole('button', { name: 'Settings', exact: true }).click();
      await page.locator('#appearance-mode').selectOption(theme);
      await page.keyboard.press('Escape');
      await expect(page.getByRole('button', { name: 'Settings', exact: true })).toBeFocused();
      if (title !== 'Unavailable') {
        const contrasts = await panel.evaluate(element => {
          const style = getComputedStyle(element);
          const luminance = (color: string) => {
            const channels = color.match(/[\d.]+/g)!.slice(0, 3).map(Number).map(value => {
              const channel = value / 255;
              return channel <= .04045 ? channel / 12.92 : ((channel + .055) / 1.055) ** 2.4;
            });
            return channels[0] * .2126 + channels[1] * .7152 + channels[2] * .0722;
          };
          const background = luminance(style.backgroundColor);
          return [...element.querySelectorAll('p, button')].map(line => {
            const foreground = luminance(getComputedStyle(line).color);
            return (Math.max(foreground, background) + .05) / (Math.min(foreground, background) + .05);
          });
        });
        expect(contrasts.length).toBeGreaterThan(0);
        for (const contrast of contrasts) expect(contrast).toBeGreaterThanOrEqual(4.5);
      }
      await page.screenshot({ path: `${dir}/${title}-${theme}.png` });
    }
    results.push({ title, state: await audioState(page) });
    writeFileSync(`${dir}/results.json`, JSON.stringify({ browser: browser.version(), results }, null, 2));
  }
});
