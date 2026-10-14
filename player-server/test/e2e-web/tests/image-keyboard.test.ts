import { test, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { bootstrap, waitForServer } from './helpers/server';

test.use({ serviceWorkers: 'block' });
let adminCookie: string;
test.beforeAll(async () => { await waitForServer(); adminCookie = await bootstrap(); });
test.beforeEach(async ({ page }) => {
  const origin = new URL(process.env.PLAYER_URL || 'http://localhost:8080');
  await page.context().addCookies([{ name: 'session', value: adminCookie.slice('session='.length), domain: origin.hostname, path: '/', sameSite: 'Strict' }]);
  await page.route(/\/api\/media\/91$/, route => route.fulfill({ json: { media: { id: 91, type: 'image', file_name: 'keyboard.svg' } } }));
  await page.goto('/');
  await expect(page.locator('#media-grid')).toBeVisible();
  await page.evaluate(async () => {
    const doc = (globalThis as any).document;
    doc.getElementById('media-grid').innerHTML = '<div class="media-card selected" tabindex="0" data-id="91">Image</div>';
    const path = '/js/playback.js';
    (await import(path)).loadMediaDirect({ id: 91, type: 'image', file_name: 'keyboard.svg' },
      'data:image/svg+xml,' + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="800"><rect width="1200" height="800" fill="red"/></svg>'), '');
  });
  await expect.poll(() => page.locator('#media-image').evaluate((img: any) => img.naturalWidth)).toBe(1200);
});

test('image controls pan, fit, enter fullscreen and give display crop precedence', async ({ page }) => {
  const image = page.locator('#media-image');
  const transform = () => image.evaluate((img: any) => img.style.transform);
  await page.keyboard.press('j');
  expect(await transform()).toBe('translate(0px, 0px) scale(1)');
  for (const modifier of ['Control', 'Meta', 'Alt']) await page.keyboard.press(`${modifier}+f`);
  expect(await page.evaluate(() => Boolean((globalThis as any).document.fullscreenElement))).toBe(false);
  await page.keyboard.press('+');
  await page.keyboard.press('j');
  await page.keyboard.press('Shift+L');
  expect(await transform()).toBe('translate(-40px, -40px) scale(1.25)');
  await page.keyboard.press('k');
  await page.keyboard.press('Shift+H');
  expect(await transform()).toBe('translate(0px, 0px) scale(1.25)');
  await page.keyboard.press('0');
  expect(await transform()).toBe('translate(0px, 0px) scale(1)');
  await page.locator('#btn-zoom-in').focus();
  await page.keyboard.press('Enter');
  expect(await transform()).toBe('translate(0px, 0px) scale(1.25)');
  await page.keyboard.press('f');
  await expect.poll(() => page.evaluate(() => Boolean((globalThis as any).document.fullscreenElement))).toBe(true);
  await page.keyboard.press('c');
  await expect(page.locator('#player')).toHaveClass(/crop-mode/);
  await page.keyboard.press('x');
  await expect(page.locator('#crop-indicator')).toHaveText('T');
  await page.keyboard.press('Shift+ArrowDown');
  await expect(page.locator('#crop-indicator')).toHaveText('50,10');
  await page.keyboard.press('j');
  await page.keyboard.press('Shift+L');
  await expect(page.locator('#crop-indicator')).toHaveText('60,20');
  expect(await transform()).toBe('translate(0px, 0px) scale(1.25)');
  await page.keyboard.press('c');
  await expect(page.locator('#player')).not.toHaveClass(/crop-mode/);
  await page.keyboard.press('j');
  expect(await transform()).toBe('translate(0px, -40px) scale(1.25)');
  await page.keyboard.press('Escape');
  await expect.poll(() => page.evaluate(() => Boolean((globalThis as any).document.fullscreenElement))).toBe(false);
  await expect(page.locator('#player')).toHaveClass(/open/);
});

test('image info, dialogs, typing and slideshow retain keyboard ownership', async ({ page }) => {
  const image = page.locator('#media-image');
  await page.keyboard.press('+');
  await page.keyboard.press('i');
  await expect(page.locator('#media-info-modal')).toHaveClass(/open/);
  await page.locator('#media-info-close').focus();
  for (const key of ['f', 'j', 'Shift+L', 'c', '0']) await page.keyboard.press(key);
  expect(await image.evaluate((img: any) => img.style.transform)).toBe('translate(0px, 0px) scale(1.25)');
  expect(await page.evaluate(() => Boolean((globalThis as any).document.fullscreenElement))).toBe(false);
  await page.keyboard.press('Enter');
  await expect(page.locator('#media-info-modal')).not.toHaveClass(/open/);
  await page.keyboard.press('Shift+S');
  await expect(page.locator('#btn-slideshow')).toHaveAttribute('aria-label', 'Pause slideshow');
  await page.keyboard.press('Shift+S');
  await expect(page.locator('#btn-slideshow')).toHaveAttribute('aria-label', 'Start slideshow');
  const search = page.locator('#search-input');
  await page.evaluate(async () => { const path = '/js/views/help.js'; (await import(path)).showSearch(); });
  await expect(search).toBeVisible();
  await search.focus();
  await page.keyboard.type('fjkc0');
  await expect(search).toHaveValue('fjkc0');
  expect(await image.evaluate((img: any) => img.style.transform)).toBe('translate(0px, 0px) scale(1.25)');
  await page.keyboard.press('Escape');
  await expect(search).not.toBeVisible();
  await expect(page.locator('#player')).toHaveClass(/open/);
  await page.keyboard.press('Escape');
  await expect(page.locator('#player')).not.toHaveClass(/open/);
});

test('fullscreen audio h/l seek remains unchanged after image controls', async ({ page }) => {
  await page.route('/image-keyboard-audio', route => route.fulfill({
    contentType: 'audio/mpeg', body: readFileSync(resolve(__dirname, '../../../testdata/media/audiobooks/aesops-fables/10-chapter.mp3')),
  }));
  await page.evaluate(async () => {
    const path = '/js/playback.js';
    const response = await fetch('/image-keyboard-audio');
    const url = URL.createObjectURL(await response.blob());
    (await import(path)).loadMediaDirect({ id: 92, type: 'audio', file_name: 'keyboard.mp3' }, url, '');
  });
  const audio = page.locator('#media-audio');
  await expect.poll(() => audio.evaluate((media: any) => media.readyState)).toBeGreaterThanOrEqual(2);
  await expect.poll(() => audio.evaluate((media: any) => media.duration)).toBeGreaterThan(20);
  await audio.evaluate((media: any) => { media.pause(); media.currentTime = 20; });
  await expect.poll(() => audio.evaluate((media: any) => media.currentTime)).toBe(20);
  await page.locator('#btn-fullscreen').click();
  await expect.poll(() => page.evaluate(() => Boolean((globalThis as any).document.fullscreenElement))).toBe(true);
  await page.keyboard.press('h');
  await expect.poll(() => audio.evaluate((media: any) => media.currentTime)).toBe(15);
  await page.keyboard.press('l');
  await expect.poll(() => audio.evaluate((media: any) => media.currentTime)).toBe(20);
  await page.keyboard.press('Escape');
});

test('fullscreen image info is visible and restores its image-control opener', async ({ page }) => {
  const player = page.locator('#player');
  const modal = page.locator('#media-info-modal');
  const opener = page.locator('#btn-zoom-in');
  const homeParent = await modal.evaluate(el => el.parentElement!.tagName);
  await opener.focus();
  await page.keyboard.press('+');
  await page.keyboard.press('j');
  const transform = await page.locator('#media-image').evaluate((image: any) => image.style.transform);
  await page.keyboard.press('f');
  await expect.poll(() => page.evaluate(() => Boolean((globalThis as any).document.fullscreenElement))).toBe(true);
  await page.keyboard.press('c');
  await page.keyboard.press('Shift+ArrowDown');
  const crop = await page.locator('#crop-indicator').textContent();
  const assertVisibleInfo = async () => {
    await expect(modal).toHaveClass(/open/);
    await expect.poll(() => modal.evaluate(el => {
      const panel = el.querySelector('.modal')!;
      const rect = panel.getBoundingClientRect();
      return el.contains(el.ownerDocument.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2));
    })).toBe(true);
    await expect.poll(() => modal.evaluate(el => el.contains(el.ownerDocument.activeElement))).toBe(true);
  };
  const assertReturned = async () => {
    await expect(modal).not.toHaveClass(/open/);
    await expect(opener).toBeFocused();
    await expect.poll(() => modal.evaluate(el => el.parentElement!.tagName)).toBe(homeParent);
    await expect.poll(() => page.evaluate(() => Boolean((globalThis as any).document.fullscreenElement))).toBe(true);
    await expect(player).toHaveClass(/crop-mode/);
    expect(await page.locator('#crop-indicator').textContent()).toBe(crop);
    expect(await page.locator('#media-image').evaluate((image: any) => image.style.transform)).toBe(transform);
  };
  await page.keyboard.press('i');
  await assertVisibleInfo();
  await page.locator('#media-info-close').focus();
  await page.keyboard.press('Tab');
  await expect.poll(() => modal.evaluate(el => el.contains(el.ownerDocument.activeElement))).toBe(true);
  await page.locator('#media-info-close').focus();
  await page.keyboard.press('Enter');
  await assertReturned();

  await page.keyboard.press('i');
  await assertVisibleInfo();
  await modal.evaluate(el => {
    const input = el.ownerDocument.createElement('textarea');
    input.id = 'fullscreen-info-editable';
    el.querySelector('.modal')!.appendChild(input);
    input.focus();
  });
  await page.keyboard.press('Escape');
  await assertReturned();

  await page.keyboard.press('i');
  await assertVisibleInfo();
  await page.locator('#media-info-close').focus();
  await page.keyboard.press('Space');
  await assertReturned();

  await page.keyboard.press('i');
  await assertVisibleInfo();
  await page.evaluate(() => (globalThis as any).document.exitFullscreen());
  await expect.poll(() => modal.evaluate(el => el.parentElement!.tagName)).toBe(homeParent);
  await assertVisibleInfo();
  await page.keyboard.press('Escape');
  await expect(modal).not.toHaveClass(/open/);
  await expect(opener).toBeFocused();
  await expect(player).toHaveClass(/open/);
});
