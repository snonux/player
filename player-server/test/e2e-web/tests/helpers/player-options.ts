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

const ACTIONS = new Set(['click', 'dblclick', 'check', 'uncheck', 'setChecked', 'selectOption', 'fill', 'focus', 'press', 'pressSequentially', 'hover', 'tap', 'type']);
const CHAINS = new Set(['locator', 'getByRole', 'getByLabel', 'getByText', 'getByTitle', 'getByTestId', 'filter', 'first', 'last', 'nth', 'and', 'or']);

export async function openPlayerOptions(page: Page): Promise<void> {
  const menu = page.locator('#player-options');
  if (!(await menu.count())) return;
  const open = await menu.evaluate(el => el.matches(':popover-open') || (!el.hasAttribute('popover') && !el.hidden)).catch(() => false);
  if (!open) await page.locator('#btn-player-options').click();
}

/**
 * Returns `locator` for a control in the player options popover. The popover
 * light-dismisses on any outside interaction, so every action on the returned
 * locator (and locators chained from it) reopens the menu first when needed.
 */
export async function inPlayerOptions(page: Page, locator: Locator): Promise<Locator> {
  const wrap = (target: Locator): Locator => new Proxy(target, {
    get(obj, key, receiver) {
      const value = Reflect.get(obj, key, receiver);
      if (typeof key !== 'string' || typeof value !== 'function') return value;
      if (ACTIONS.has(key)) return async (...args: unknown[]) => {
        if (!(await obj.first().isVisible())) await openPlayerOptions(page);
        return (value as Function).apply(obj, args);
      };
      // Closed popover content is hidden, so chained role queries include
      // hidden elements; assertions then still find the control's status.
      if (key === 'getByRole') return (role: string, options: Record<string, unknown> = {}) =>
        wrap(obj.getByRole(role as never, { includeHidden: true, ...options }));
      if (CHAINS.has(key)) return (...args: unknown[]) => wrap((value as Function).apply(obj, args));
      return value.bind(obj);
    },
  });
  if (!(await locator.first().isVisible())) await openPlayerOptions(page);
  return wrap(locator);
}

/** Restore a minimized player; starting new playback may already have restored it. */
export async function restorePlayer(page: Page): Promise<void> {
  const restore = page.getByRole('button', { name: 'Restore player', exact: true });
  if (await restore.isVisible()) await restore.click();
}

/** Close the options popover so it does not cover the seek bar or cards. */
export async function closePlayerOptions(page: Page): Promise<void> {
  await page.locator('#player-options').evaluate(el => {
    if (el.matches(':popover-open')) (el as HTMLElement).hidePopover();
  }).catch(() => {});
}
