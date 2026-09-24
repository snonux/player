import { test, expect } from '@playwright/test';
import { bootstrap, waitForServer } from './helpers/server';

test.use({ serviceWorkers: 'block' });

let adminCookie: string;

test.beforeAll(async () => {
  await waitForServer();
  adminCookie = await bootstrap();
});

test('visible Minimize and Restore buttons work for audio, video, and image', async ({ page }) => {
  const origin = new URL(process.env.PLAYER_URL || 'http://localhost:8080');
  await page.context().addCookies([{
    name: 'session', value: adminCookie.slice('session='.length),
    domain: origin.hostname, path: '/', sameSite: 'Strict',
  }]);
  await page.goto('/');

  const player = page.locator('#player');
  const minimize = page.locator('#btn-minimize');
  const restore = page.locator('#btn-restore-player');
  for (const [type, fileName, url] of [
    ['audio', 'sample.mp3', 'data:audio/mpeg;base64,AAAA'],
    ['video', 'sample.mp4', 'data:video/mp4;base64,AAAA'],
    ['image', 'sample.gif', 'data:image/gif;base64,R0lGODlhAQABAAD/ACwAAAAAAQABAAACADs='],
  ]) {
    await page.evaluate(async args => {
      const modulePath = '/js/playback.js';
      const { loadMediaDirect } = await import(modulePath);
      loadMediaDirect({ id: 1, type: args.type, file_name: args.fileName, duration: 1 }, args.url, '');
    }, { type, fileName, url });
    await expect(player).toHaveClass(/open/);
    await expect(minimize).toBeVisible();
    await minimize.click();
    await expect(player).toHaveClass(/minimized/);
    await expect(restore).toBeVisible();
    await expect(restore).toContainText(`Restore: ${fileName}`);
    await restore.click();
    await expect(player).not.toHaveClass(/minimized/);
    await expect(minimize).toBeVisible();
  }
});
