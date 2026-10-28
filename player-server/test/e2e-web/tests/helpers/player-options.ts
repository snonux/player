import type { Locator, Page } from '@playwright/test';

/** Secondary floating-player controls live in the ⋯ options menu. */
export async function playerOptionsItem(page: Page, selector: string): Promise<Locator> {
  const item = page.locator(selector);
  if (!(await item.isVisible())) {
    const menu = page.locator('#player-options');
    const open = await menu.evaluate(el => el.matches(':popover-open') || (!el.hasAttribute('popover') && !el.hidden)).catch(() => false);
    if (!open) await page.locator('#btn-player-options').click();
  }
  return item;
}

/** Minimize via the options menu (Shift+C remains the keyboard path). */
export async function minimizePlayer(page: Page): Promise<void> {
  const minimize = await playerOptionsItem(page, '#btn-minimize');
  await minimize.click();
}
