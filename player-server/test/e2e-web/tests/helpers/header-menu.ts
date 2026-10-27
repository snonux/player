import type { Locator, Page } from '@playwright/test';

/** Secondary header controls live in the "More options" popover menu. */
export async function headerItem(page: Page, selector: string): Promise<Locator> {
  const item = page.locator(selector);
  if (!(await item.isVisible())) {
    const menu = page.locator('#header-menu');
    if (!(await menu.evaluate(el => el.matches(':popover-open')))) await page.locator('#header-menu-toggle').click();
  }
  return item;
}
