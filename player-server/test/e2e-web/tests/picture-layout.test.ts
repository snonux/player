import { test, expect, Page } from '@playwright/test';
import { bootstrap, waitForServer } from './helpers/server';

test.use({ serviceWorkers: 'block' });
const baseURL = process.env.PLAYER_URL || 'http://localhost:8080';
let cookie: string;
const dimensions = [[800, 400], [400, 800], [0, 0], [600, 600], [1200, 400], [400, 800], [800, 400], [600, 600]];
const media = dimensions.map(([width, height], index) => ({
  id: index + 1, type: 'image', file_name: `image-${index + 1}.jpg`,
  width, height, thumbnail_path: 'fixture.jpg', rel_path: `image-${index + 1}.jpg`,
}));
media.push({ id: 9, type: 'video', file_name: 'video.mp4', width: 800, height: 400, thumbnail_path: 'fixture.jpg', rel_path: 'video.mp4' });
media.push({ id: 10, type: 'audio', file_name: 'audio.mp3', width: 0, height: 0, thumbnail_path: '', rel_path: 'audio.mp3' });

test.beforeAll(async () => { await waitForServer(); cookie = await bootstrap(); });
test.beforeEach(async ({ page }) => {
  await page.context().addCookies([{
    name: 'session', value: cookie.slice('session='.length), domain: new URL(baseURL).hostname, path: '/',
  }]);
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/thumbnail')) {
      const id = Number(path.split('/')[3]);
      const [width, height] = dimensions[id - 1] || [800, 400];
      await route.fulfill({ contentType: 'image/svg+xml', body: `<svg xmlns="http://www.w3.org/2000/svg" width="${width || 600}" height="${height || 600}" viewBox="0 0 ${width || 600} ${height || 600}"><rect width="100%" height="100%" fill="teal"/><rect x="1" y="1" width="${(width || 600) - 2}" height="${(height || 600) - 2}" fill="none" stroke="white" stroke-width="8"/></svg>` });
      return;
    }
    let body: unknown = [];
    if (path === '/api/config') body = { media_page_size: 9 };
    if (path === '/api/sets') body = [{ id: 1, name: 'pictures' }];
    if (path.endsWith('/browse')) body = { current_path: '', folders: [{ name: 'folder', has_cover: false }], media };
    await route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Set pictures', exact: true }).click();
  await expect(page.locator('#media-grid .image-card')).toHaveCount(8);
});

async function boxes(page: Page) {
  return page.locator('#media-grid > .media-card').evaluateAll(cards => cards.map(card => {
    const rect = card.getBoundingClientRect();
    const thumb = card.querySelector('.thumb-wrap')!.getBoundingClientRect();
    return { id: (card as HTMLElement).dataset.id || 'folder', top: rect.top, left: rect.left, width: rect.width, height: thumb.height };
  }));
}

test('picture rows fit metadata, preserve order/selection and native toggle, and paginate mixed media', async ({ page }, testInfo) => {
  const cards = page.locator('#media-grid > .media-card');
  const before = await boxes(page);
  await cards.nth(2).focus();
  const selectedId = await page.locator('.media-card.selected').getAttribute('data-id');
  const toggle = page.getByRole('button', { name: 'Picture layout', exact: true });
  await toggle.focus();
  await page.keyboard.press('Enter');
  await expect(toggle).toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('#media-grid')).toHaveClass(/picture-grid/);
  expect(await page.locator('.media-card.selected').getAttribute('data-id')).toBe(selectedId);
  const after = await boxes(page);
  expect(after.map(card => card.id)).toEqual(before.map(card => card.id));
  for (const [id, ratio] of [['1', 2], ['2', 0.5], ['3', 1]] as const) {
    const box = after.find(card => card.id === id)!;
    expect((box.width - 2) / box.height).toBeCloseTo(ratio, 2);
  }
  await expect(page.locator('.image-card img').first()).toHaveCSS('object-fit', 'contain');
  expect(await page.locator('#media-grid [style]').count()).toBe(0);
  await page.screenshot({ path: testInfo.outputPath('picture-layout-desktop.png'), fullPage: true });

  // First/last edges and irregular rows use their real positions and centers.
  await cards.first().locator('.folder-open').focus();
  await page.keyboard.press('ArrowUp');
  await expect(cards.first()).toHaveClass(/selected/);
  const nextTop = after.find(box => box.top > after[0].top + 1)!.top;
  const center = after[0].left + after[0].width / 2;
  const expected = after.filter(box => Math.abs(box.top - nextTop) < 1).sort((a, b) => Math.abs(a.left + a.width / 2 - center) - Math.abs(b.left + b.width / 2 - center))[0];
  await page.keyboard.press('ArrowDown');
  await expect(page.locator('.media-card.selected')).toHaveAttribute('data-id', expected.id);
  await cards.last().focus();
  await page.keyboard.press('ArrowDown');
  await expect(cards.last()).toHaveClass(/selected/);

  await page.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(page.locator('#media-grid .image-card')).toHaveCount(0);
  await expect(page.locator('#media-grid .media-card')).toHaveCount(2);
  await expect(page.locator('#media-grid')).not.toHaveClass(/picture-grid/);
  await page.getByRole('button', { name: 'Prev', exact: true }).click();
  await expect(page.locator('#media-grid')).toHaveClass(/picture-grid/);
  await toggle.click();
  await expect(page.locator('#media-grid')).not.toHaveClass(/picture-grid/);
});

test('picture preference survives reload and narrow reflow has no overflow or lost keyboard cards', async ({ page }, testInfo) => {
  await page.getByRole('button', { name: 'Picture layout', exact: true }).click();
  await page.reload();
  await expect(page.getByRole('button', { name: 'Picture layout', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('#media-grid')).not.toHaveClass(/picture-grid/); // Sets remain regular cards.
  await page.getByRole('button', { name: 'Set pictures', exact: true }).click();
  await page.setViewportSize({ width: 320, height: 640 });
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(320);
  await expect.poll(async () => (await boxes(page)).every(box => box.left + box.width <= 320)).toBe(true);
  const cards = page.locator('#media-grid > .media-card');
  await cards.first().locator('.folder-open').focus();
  for (let i = 1; i < 9; i++) {
    await page.keyboard.press('ArrowRight');
    await expect(cards.nth(i)).toHaveClass(/selected/);
  }
  await page.keyboard.press('ArrowRight');
  await expect(cards.last()).toHaveClass(/selected/);
  await page.screenshot({ path: testInfo.outputPath('picture-layout-mobile.png'), fullPage: true });
});
