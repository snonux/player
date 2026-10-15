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

test('preview keys clamp, preserve selection and persist; controls keep native Enter', async ({ page }) => {
  const grid = page.locator('#media-grid');
  const card = grid.locator('.image-card').first();
  await card.focus();
  const selected = await grid.locator('.selected').getAttribute('data-id');
  const original = (await card.boundingBox())!.width;
  await page.keyboard.press('+');
  await page.keyboard.press('=');
  await expect(grid).toHaveAttribute('data-thumbnail-size', '4');
  expect((await card.boundingBox())!.width).toBeGreaterThan(original);
  await expect(card).toBeFocused();
  await expect(grid.locator('.selected')).toHaveAttribute('data-id', selected!);
  await expect(page.getByRole('button', { name: 'Larger previews', exact: true })).toBeDisabled();
  await page.keyboard.press('+');
  await expect(grid).toHaveAttribute('data-thumbnail-size', '4');
  for (let i = 0; i < 8; i++) await page.keyboard.press('-');
  await expect(grid).toHaveAttribute('data-thumbnail-size', '0');
  expect((await card.boundingBox())!.width).toBeLessThan(original);
  await expect(page.getByRole('button', { name: 'Smaller previews', exact: true })).toBeDisabled();
  await page.reload();
  await expect(grid).toHaveAttribute('data-thumbnail-size', '0');
  await page.getByRole('button', { name: 'Set pictures', exact: true }).click();
  await page.getByRole('button', { name: 'Larger previews', exact: true }).focus();
  await page.keyboard.press('Enter');
  await expect(grid).toHaveAttribute('data-thumbnail-size', '1');
  await page.keyboard.press('0');
  await expect(grid).toHaveAttribute('data-thumbnail-size', '2');
  await page.getByRole('button', { name: 'Larger previews', exact: true }).click();
  await page.getByRole('button', { name: 'Larger previews', exact: true }).click();
  await expect(page.locator('#thumbnail-reset')).toBeFocused();
  for (let i = 0; i < 4; i++) await page.getByRole('button', { name: 'Smaller previews', exact: true }).click();
  await expect(page.locator('#thumbnail-reset')).toBeFocused();
});

test('preview keys respect typing, dialogs, browser chords and image zoom', async ({ page }) => {
  const reset = page.locator('#thumbnail-reset');
  await page.keyboard.press('/');
  await page.keyboard.type('+-0');
  await expect(page.locator('#search-input')).toHaveValue('+-0');
  await expect(reset).toHaveText('100%');
  await page.keyboard.press('Escape');
  await page.keyboard.press('?');
  await page.locator('#help-close').focus();
  for (const key of ['+', '-', '0']) await page.keyboard.press(key);
  await expect(reset).toHaveText('100%');
  await page.keyboard.press('Escape');
  for (const modifier of ['Control', 'Meta', 'Alt']) {
    for (const key of ['=', '-', '0']) await page.keyboard.press(`${modifier}+${key}`);
  }
  await expect(reset).toHaveText('100%');
  await page.evaluate(async () => {
    const path = '/js/playback.js';
    (await import(path)).loadMediaDirect({ id: 1, type: 'image', file_name: 'zoom.svg' },
      'data:image/svg+xml,' + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="800"><rect width="1200" height="800" fill="teal"/></svg>'), '');
  });
  await expect.poll(() => page.locator('#media-image').evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(1200);
  await page.keyboard.press('+');
  await expect(page.locator('#media-image')).toHaveCSS('transform', /1.25/);
  await expect(reset).toHaveText('100%');
  await page.keyboard.press('0');
  await expect(page.locator('#media-image')).toHaveCSS('transform', 'matrix(1, 0, 0, 1, 0, 0)');
});

test('preview buttons resize picture rows and fit narrow screens at every size', async ({ page }, testInfo) => {
  await page.keyboard.press('g');
  const before = await boxes(page);
  await page.getByRole('button', { name: 'Larger previews', exact: true }).click();
  await expect.poll(async () => (await boxes(page))[1].height).toBeGreaterThan(before[1].height);
  await page.reload();
  await page.getByRole('button', { name: 'Set pictures', exact: true }).click();
  await expect(page.locator('#thumbnail-reset')).toHaveText('130%');
  await expect(page.locator('#media-grid')).toHaveClass(/picture-grid/);
  await page.setViewportSize({ width: 320, height: 640 });
  for (const picture of [true, false]) {
    if (!picture) { await page.locator('#thumbnail-reset').focus(); await page.keyboard.press('g'); }
    for (let size = 0; size <= 4; size++) {
      await page.evaluate((index) => localStorage.setItem('kiss-thumbnail-size', String(index)), size);
      await page.reload();
      await page.getByRole('button', { name: 'Set pictures', exact: true }).click();
      await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(320);
      await expect.poll(async () => (await boxes(page)).every(box => box.left + box.width <= 320)).toBe(true);
    }
  }
  await page.locator('#thumbnail-reset').click();
  await expect(page.locator('#thumbnail-reset')).toHaveText('100%');
  await page.screenshot({ path: testInfo.outputPath('preview-size-mobile.png'), fullPage: true });
});

test('invalid or unavailable preview storage leaves controls usable', async ({ page }) => {
  await page.evaluate(() => localStorage.setItem('kiss-thumbnail-size', '99'));
  await page.reload();
  await expect(page.locator('#thumbnail-reset')).toHaveText('100%');
  await page.addInitScript(() => {
    Storage.prototype.getItem = () => { throw new DOMException('Storage blocked', 'SecurityError'); };
    Storage.prototype.setItem = () => { throw new DOMException('Storage blocked', 'SecurityError'); };
  });
  await page.reload();
  await page.getByRole('button', { name: 'Larger previews', exact: true }).click();
  await expect(page.locator('#thumbnail-reset')).toHaveText('130%');
  await page.keyboard.press('0');
  await expect(page.locator('#thumbnail-reset')).toHaveText('100%');
});

test('every action stays inside and clickable on small regular and picture previews', async ({ page }) => {
  await page.route('**/api/media/2', route => route.fulfill({ json: { media: media[1] } }));
  await page.route('**/api/media/2/notes', route => route.fulfill({ json: { content: '' } }));
  page.on('dialog', dialog => dialog.dismiss()); // Exercise the trash action without deleting the fixture.
  page.on('popup', popup => popup.close());
  await page.evaluate(() => {
    (window as any).clickedActions = [];
    document.addEventListener('click', event => {
      const action = (event.target as Element).closest<HTMLElement>('[data-action]')?.dataset.action;
      if (action) (window as any).clickedActions.push(action);
    }, true);
  });
  const actions = ['play', 'favorite', 'notes', 'trash', 'mark-finished', 'mark-not-started', 'download', 'tags', 'regen-thumb'];
  for (const width of [1280, 320]) {
    await page.setViewportSize({ width, height: 800 });
    for (const picture of [false, true]) {
      await page.locator('#thumbnail-reset').focus();
      if ((await page.locator('#media-grid').getAttribute('class'))!.includes('picture-grid') !== picture) await page.keyboard.press('g');
      for (let i = 0; i < 4; i++) await page.keyboard.press('-');
      const card = page.locator('#media-grid .image-card[data-id="2"]');
      for (const action of actions) {
        const button = card.locator(`[data-action="${action}"]`);
        await button.scrollIntoViewIfNeeded();
        const bounds = await card.boundingBox();
        const target = (await button.boundingBox())!;
        expect(target.x).toBeGreaterThanOrEqual(bounds!.x);
        expect(target.x + target.width).toBeLessThanOrEqual(bounds!.x + bounds!.width + 1);
        expect(target.y).toBeGreaterThanOrEqual(bounds!.y);
        expect(target.y + target.height).toBeLessThanOrEqual(bounds!.y + bounds!.height + 1);
        await expect.poll(() => button.evaluate(el => {
          const r = el.getBoundingClientRect();
          return el.contains(document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2));
        }), `${width}px ${picture ? 'picture' : 'regular'} ${action} hit target`).toBe(true);
        await button.click();
        await expect.poll(() => page.evaluate(() => (window as any).clickedActions.at(-1))).toBe(action);
        if (action === 'notes' || action === 'tags') {
          await expect(page.locator(`#${action}-modal`)).toHaveClass(/open/);
          await page.keyboard.press('Escape');
          await expect(page.locator(`#${action}-modal`)).toBeHidden();
        }
        if (action === 'play') {
          await expect(page.locator('#player')).toHaveClass(/open/);
          await page.keyboard.press('Escape');
        }
      }
    }
  }
});

test('picture rows fit metadata, preserve order/selection and shortcut, and paginate mixed media', async ({ page }, testInfo) => {
  const cards = page.locator('#media-grid > .media-card');
  const before = await boxes(page);
  await cards.nth(2).focus();
  const selectedId = await page.locator('.media-card.selected').getAttribute('data-id');
  await page.keyboard.press('g');
  await expect(page.locator('#picture-layout-toggle')).toHaveAttribute('aria-pressed', 'true');
  await expect(cards.nth(2)).toBeFocused();
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
  await page.keyboard.press('g');
  await expect(page.locator('#media-grid')).not.toHaveClass(/picture-grid/);
});

test('picture preference survives reload and narrow reflow has no overflow or lost keyboard cards', async ({ page }, testInfo) => {
  await page.keyboard.press('g');
  await page.reload();
  await expect(page.locator('#picture-layout-toggle')).toHaveAttribute('aria-pressed', 'true');
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


test('help hint stays visible, documents g and offers a native picture toggle', async ({ page }) => {
  const hint = page.getByRole('button', { name: 'Press ? for help', exact: true });
  await page.mouse.move(600, 400); // Header stays retracted on desktop.
  await expect(hint).toBeInViewport();
  await expect(page.locator('.browse-toolbar #picture-layout-toggle')).toHaveCount(0);
  await hint.click();
  const help = page.locator('#help-modal');
  await expect(help).toHaveClass(/open/);
  await expect(help.locator('.help-row').filter({ hasText: 'Picture layout' })).toContainText('g');
  const toggle = help.getByRole('button', { name: 'Picture layout', exact: true });
  await toggle.focus();
  await page.keyboard.press('Enter');
  await expect(toggle).toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('#media-grid')).toHaveClass(/picture-grid/);
  await page.keyboard.press('g'); // Dialog owns letter keys.
  await expect(toggle).toHaveAttribute('aria-pressed', 'true');
  await page.keyboard.press('Escape');
  await expect(hint).toBeFocused();
  await page.keyboard.press('?');
  await expect(help).toHaveClass(/open/);
  await page.keyboard.press('Escape');
  await page.setViewportSize({ width: 320, height: 640 });
  await expect(hint).toBeInViewport();
});

test('picture shortcut respects typing, browser chords', async ({ page }) => {
  const grid = page.locator('#media-grid');
  await page.keyboard.press('/');
  const search = page.locator('#search-input');
  await expect(search).toBeFocused();
  await page.keyboard.press('g');
  await expect(search).toHaveValue('g');
  await expect(grid).not.toHaveClass(/picture-grid/);
  await page.keyboard.press('Escape');
  await page.keyboard.press('Control+g');
  await page.keyboard.press('Alt+g');
  await page.keyboard.press('Meta+g');
  await expect(page.locator('#picture-layout-toggle')).toHaveAttribute('aria-pressed', 'false');
  await page.keyboard.press('g');
  await expect(page.locator('#picture-layout-toggle')).toHaveAttribute('aria-pressed', 'true');
});
