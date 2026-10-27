import type { Locator, Page } from '@playwright/test';

/** Runs a secondary media-card action through the card's "More actions" menu. */
export async function cardAction(page: Page, card: Locator, name: string): Promise<void> {
  await card.hover();
  await card.locator('[data-action="more"]').click();
  await page.locator('.shortcut-picker[aria-label="Media actions"]')
    .getByRole('button', { name, exact: true }).click();
}
