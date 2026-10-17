import { test, expect, type Page, type Locator } from '@playwright/test';
import { mockLibrary, album } from './helpers/mock-library';

test.use({ serviceWorkers: 'block' });
test.beforeEach(async ({ context }) => { await mockLibrary(context); });

async function keyFor(page: Page, control: Locator) {
  await expect(control).toHaveAttribute('data-shortcut-key', /^[a-z]+$/);
  const key = await control.getAttribute('data-shortcut-key');
  await page.keyboard.type(key!);
}

test('all visible controls have unique shortcuts; typed text and browser chords retain their behavior', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Set Music', exact: true }).waitFor();
  await page.keyboard.press(';');
  const buttons = page.locator('button:visible').filter({ hasNot: page.locator('#keyboard-access-toggle') });
  for (const button of await buttons.all()) if (await button.getAttribute('id') !== 'keyboard-access-toggle') await expect(button).toHaveAttribute('data-shortcut-key');
  const keys = await page.locator('[data-shortcut-key]').evaluateAll(elements => elements.map(element => element.getAttribute('data-shortcut-key')));
  expect(new Set(keys).size).toBe(keys.length);
  await keyFor(page, page.getByRole('button', { name: 'Set Music', exact: true }));
  await expect(page).toHaveURL(/set=1/);
  await page.keyboard.press('/');
  const input = page.getByRole('searchbox', { name: 'Search media' });
  await expect(input).toBeFocused();
  await page.keyboard.type('n;f2abc');
  await expect(input).toHaveValue('n;f2abc');
  await page.keyboard.press('Control+a');
  await page.keyboard.type('text');
  await expect(input).toHaveValue('text');
  await page.keyboard.press('F2');
  await expect(page.locator('.shortcut-status')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(input).toHaveValue('text');
});

for (const width of [390, 1280]) {
  test(`player controls, visual choices, range keys and notes are usable without a mouse at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 800 });
    await page.goto('/?set=1&folder=' + encodeURIComponent(album) + '&media=11');
    await expect(page).toHaveTitle('Track 1.wav — Player');
    await page.keyboard.press('o');
    const volume = page.getByRole('slider', { name: 'Volume', exact: true });
    await keyFor(page, volume);
    await expect(volume).toBeFocused();
    await page.keyboard.press('ArrowLeft');
    await expect.poll(() => page.locator('#media-audio').evaluate((a: HTMLAudioElement) => a.volume)).toBeLessThan(1);
    if (width < 600) await page.keyboard.press('Escape');
    await page.keyboard.press('o');
    await keyFor(page, page.getByRole('combobox', { name: 'Audio visualization' }));
    const choices = page.getByRole('group', { name: 'Audio visualization', exact: true });
    await expect(choices).toBeVisible();
    await keyFor(page, choices.getByRole('button', { name: 'Off', exact: true }));
    await expect(page.locator('select[aria-label="Audio visualization"]')).toHaveValue('off');
    if (width >= 600) await page.keyboard.press('Escape'); // release the inline native select's focus
    // Escape may minimize the player after leaving a native field.
    if (await page.locator('#player').evaluate(el => el.classList.contains('minimized'))) await page.locator('#btn-restore-player').press('Enter');
    await page.keyboard.press('o');
    await keyFor(page, page.locator('#btn-player-notes'));
    await expect(page.locator('#notes-modal')).toHaveClass(/open/);
    const area = page.getByRole('textbox');
    await expect(area).toBeFocused();
    await page.keyboard.type('p n s ; should stay literal');
    await expect(area).toHaveValue('p n s ; should stay literal');
    await page.keyboard.press('F2');
    await keyFor(page, page.getByRole('button', { name: 'Save', exact: true }));
    await expect(page.locator('#notes-modal')).not.toHaveClass(/open/);
    await expect.poll(() => page.locator('#media-audio').evaluate((a: HTMLAudioElement) => a.paused)).toBe(true);
  });
}

test('roomy windows show every control inline; resizing keeps the same controls and values', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto('/?media=11');
  await expect(page.locator('#player')).toHaveClass(/controls-expanded/);
  await expect(page.locator('#btn-player-options')).toBeHidden();
  for (const id of ['btn-save-position', 'btn-bookmarks', 'btn-player-notes', 'btn-player-favorite', 'volume-slider']) await expect(page.locator('#' + id)).toBeVisible();
  await page.locator('#volume-slider').fill('0.4');
  await page.setViewportSize({ width: 390, height: 800 });
  await expect(page.locator('#btn-player-options')).toBeVisible();
  await page.keyboard.press('Escape');
  await page.keyboard.press('o');
  await expect(page.locator('#volume-slider')).toHaveValue('0.4');
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('Tab');
  await expect(page.locator('#player-options')).toContainText('Player controls');
  await page.screenshot({ path: '/tmp/player-next-shortcuts-menu.png' });
});

test('thumbnail controls appear only on hover in one row; keyboard actions remain available', async ({ page }) => {
  await page.setViewportSize({ width: 800, height: 800 });
  await page.goto('/?set=1&folder=' + encodeURIComponent(album));
  const card = page.locator('[data-id="11"]');
  const actions = card.locator('.card-actions');
  await expect(card).toBeVisible();
  await page.mouse.move(0, 0);
  await expect(actions).toBeHidden();
  await card.focus();
  await page.keyboard.press(';');
  await expect(actions).toBeHidden();
  await page.keyboard.press('Escape');
  await card.hover();
  await expect(actions).toBeVisible();
  const rows = await actions.locator('button').evaluateAll(buttons => buttons.map(button => Math.round(button.getBoundingClientRect().top)));
  expect(new Set(rows).size).toBe(1);
  expect(await actions.evaluate(element => getComputedStyle(element).flexWrap)).toBe('nowrap');
  await page.mouse.move(0, 0);
  await expect(actions).toBeHidden();
  await card.focus();
  await page.keyboard.press('a');
  const menu = page.getByRole('group', { name: 'Media actions', exact: true });
  await expect(menu).toBeVisible();
  expect(await menu.getByRole('button').count()).toBe(await actions.locator('button').count());
  await keyFor(page, menu.getByRole('button', { name: 'Notes', exact: true }));
  await expect(page.locator('#notes-modal')).toHaveClass(/open/);
  await expect(actions).toBeHidden();
});

test('dialog letters stay scoped to help, tags and admin controls', async ({ page }) => {
  await page.goto('/?set=1&folder=' + encodeURIComponent(album));
  await page.locator('[data-id="11"]').focus();
  await page.keyboard.press('?');
  const help = page.locator('#help-modal');
  await expect(help).toHaveClass(/open/);
  await page.keyboard.press('F2');
  await keyFor(page, page.locator('#help-search'));
  await page.keyboard.type('shortcuts');
  await expect(page.locator('#help-search')).toHaveValue('shortcuts');
  await page.keyboard.press('F2');
  await keyFor(page, page.locator('#help-close'));
  await expect(help).not.toHaveClass(/open/);
  await page.keyboard.press('t');
  await expect(page.locator('#tags-modal')).toHaveClass(/open/);
  await page.locator('#tags-new').fill('keyboard tag');
  await page.keyboard.press('F2');
  await keyFor(page, page.locator('#tags-add'));
  await page.keyboard.press('Escape');
  await page.keyboard.press('A');
  await expect(page.locator('#admin-modal')).toHaveClass(/open/);
  await page.keyboard.press('F2');
  const admin = page.locator('#admin-modal');
  for (const button of await admin.locator('button:visible:not(:disabled)').all()) await expect(button).toHaveAttribute('data-shortcut-key');
  await keyFor(page, page.locator('#admin-close'));
  await expect(admin).not.toHaveClass(/open/);
  await expect(page.locator('#player')).not.toHaveClass(/open/);
});
