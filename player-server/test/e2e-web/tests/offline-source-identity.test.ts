import { test, expect } from '@playwright/test';

// Only the settings host and authenticated identities are fixtures. Rendering,
// account scoping and cache reads use the application's actual modules.
test('saved collections name their server and account across account changes and sign-out', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.route('**/__offline-source-fixture.html', route => route.fulfill({
    contentType: 'text/html',
    body: '<!doctype html><html><head><meta name="viewport" content="width=device-width, initial-scale=1"><title>Offline source identity fixture</title></head><body><main id="settings-modal"><section class="modal"></section></main></body></html>',
  }));
  await page.goto('/__offline-source-fixture.html');
  await page.evaluate(async () => {
    for (const id of [7, 8]) {
      const url = `/saved/account-${id}.mp3`;
      const name = `player-offline-v1-${id}`;
      await (await caches.open(name)).put(url, new Response(`account ${id} bytes`));
      localStorage.setItem(`${name}:index`, JSON.stringify({
        collections: { [JSON.stringify(['album', String(id)])]: {
          title: `Account ${id} album`, pinned: true, urls: [url],
        } },
        songs: { [url]: { url, media: { id, type: 'audio' } } },
      }));
    }
    const { setAuthenticatedUser } = await import('/js/state.js');
    setAuthenticatedUser(7);
    const { initOfflinePinLibrary } = await import('/js/offline-pin-library.js');
    initOfflinePinLibrary();
  });
  const panel = page.locator('[data-offline-library]');
  const source = panel.locator('[data-source]');
  const origin = new URL(page.url()).origin;
  await expect(source).toHaveText(`${origin} · Account 7`);
  await expect(source).toBeVisible();
  await panel.getByText('Manage downloaded collections', { exact: true }).click();
  await expect(panel.getByRole('button', { name: /^Account 7 album/ })).toBeVisible();
  await expect(panel.getByRole('button', { name: /^Account 8 album/ })).toHaveCount(0);

  // An in-flight render must not place an old account's rows under a new label.
  await page.evaluate(async () => {
    const { setAuthenticatedUser } = await import('/js/state.js');
    for (const id of [7, 8]) {
      setAuthenticatedUser(id);
      document.dispatchEvent(new Event('offline-pins:changed'));
    }
  });
  await expect(source).toHaveText(`${origin} · Account 8`);
  await expect(panel.getByRole('button', { name: /^Account 8 album/ })).toBeVisible();
  await expect(panel.getByRole('button', { name: /^Account 7 album/ })).toHaveCount(0);
  await page.setViewportSize({ width: 844, height: 390 });
  await expect(source).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);

  await page.evaluate(async () => {
    const { setAuthenticatedUser } = await import('/js/state.js');
    setAuthenticatedUser(null);
    document.dispatchEvent(new Event('offline-pins:changed'));
  });
  await expect(source).toHaveText(`${origin} · Signed out`);
  await expect(panel.locator('[data-status]')).toContainText('Sign in online to unlock');
  await expect(panel.locator('[data-status]')).toContainText('Offline sign-in is not available.');
  await expect(panel.locator('[data-collections] button')).toHaveCount(0);
  await panel.getByRole('button', { name: 'Refresh saved collections', exact: true }).click();
  await expect(source).toHaveText(`${origin} · Signed out`);
  expect(await page.evaluate(() => localStorage.getItem('player-offline-v1-7:index'))).toContain('Account 7 album');
});
