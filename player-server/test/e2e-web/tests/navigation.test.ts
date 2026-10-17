import { test, expect } from '@playwright/test';
import { mockLibrary, album, tracks } from './helpers/mock-library';

test.use({ serviceWorkers: 'block' });
test.beforeEach(async ({ context }) => { await mockLibrary(context); });

test('sets, Unicode folders and media update the URL; refresh restores paused media and position', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Set Music', exact: true }).click();
  await expect(page).toHaveURL(/set=1/);
  await page.getByRole('button', { name: 'Folder Artist & Beyoncé', exact: true }).click();
  await page.getByRole('button', { name: 'Folder Album #1 日本語', exact: true }).click();
  expect(new URL(page.url()).searchParams.get('folder')).toBe(album);
  await page.locator('[data-id="11"]').hover();
  await page.locator('[data-id="11"] [data-action="play"]').click();
  await expect(page).toHaveURL(/media=11/);
  const link = page.url();
  await page.reload();
  await expect(page.locator('#player')).toHaveClass(/open/);
  await expect(page).toHaveTitle('Track 1.wav — Player');
  await expect(page.locator('[data-id="11"]')).toHaveClass(/selected/);
  await expect.poll(() => page.locator('#media-audio').evaluate((audio: HTMLAudioElement) => ({ paused: audio.paused, position: audio.currentTime }))).toEqual({ paused: true, position: 5 });
  expect(page.url()).toBe(link);
});

test('a media-only URL opens the right folder and page in a fresh browser context', async ({ browser }) => {
  const context = await browser.newContext({ serviceWorkers: 'block' });
  await mockLibrary(context, 2);
  try {
    const page = await context.newPage();
    await page.goto('/?media=15');
    await expect(page).toHaveTitle('Track 5.wav — Player');
    expect(new URL(page.url()).searchParams.get('folder')).toBe(album);
    await expect(page.locator('[data-id="15"]')).toBeVisible();
    expect(new URL(page.url()).searchParams.get('page')).toBe('2');
    await page.reload();
    await expect(page.locator('[data-id="15"]')).toHaveClass(/selected/);
  } finally { await context.close(); }
});

test('Back and Forward restore the selected track without autoplay or extra history entries', async ({ page }) => {
  await page.goto('/?set=1&folder=' + encodeURIComponent(album));
  await page.locator('[data-id="11"]').hover();
  await page.locator('[data-id="11"] [data-action="play"]').click();
  await expect(page).toHaveURL(/media=11/);
  await page.locator('#btn-next').click();
  await expect(page).toHaveURL(/media=12/);
  await page.goBack();
  await expect(page).toHaveTitle('Track 1.wav — Player');
  await expect.poll(() => page.locator('#media-audio').evaluate((audio: HTMLAudioElement) => audio.paused)).toBe(true);
  await page.goBack();
  await expect(page.locator('#player')).not.toHaveClass(/open/);
  await expect(page).not.toHaveURL(/media=/);
  await page.goForward();
  await expect(page).toHaveTitle('Track 1.wav — Player');
});

test('a late detail request cannot reopen an old track after Back', async ({ page }) => {
  let release!: () => void;
  await page.route('**/api/media/12', async route => {
    await new Promise<void>(resolve => { release = resolve; });
    await route.fulfill({ json: { media: tracks[1], progress: null } });
  });
  await page.goto('/?set=1&folder=' + encodeURIComponent(album));
  await page.locator('[data-id="11"]').focus();
  await page.keyboard.press('Enter');
  await expect(page).toHaveTitle('Track 1.wav — Player');
  await page.locator('#btn-next').click();
  await expect.poll(() => Boolean(release)).toBe(true);
  await page.goBack();
  await expect(page.locator('#player')).not.toHaveClass(/open/);
  release();
  await expect(page).not.toHaveURL(/media=/);
  await expect(page.locator('#player')).not.toHaveClass(/open/);
});

test('missing linked media shows an error and retry instead of opening an unrelated item', async ({ page }) => {
  await page.goto('/?media=999');
  await expect(page.getByRole('alert')).toContainText('Media not found');
  await expect(page.getByRole('button', { name: 'Retry link' })).toBeVisible();
  await expect(page.locator('#player')).not.toHaveClass(/open/);
  await expect(page).toHaveURL(/media=999/);
});
