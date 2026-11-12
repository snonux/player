import { test, expect } from '@playwright/test';
import { bootstrap, waitForServer, ADMIN_USER, ADMIN_PASS } from './helpers/server';

// The signed-in "Continue listening" banner opens every podcast episode in
// progress, not only the last one. Episodes come from a stubbed listing so the
// test needs no publisher or podcast fixture.
test.beforeAll(async () => {
  await waitForServer(15_000);
  await bootstrap();
});

const episode = (n: number, position: number) => ({
  id: 9000 + n, feed_key: 'feed:https://feeds.player-e2e.test/progress.xml', episode_key: `guid:progress-${n}`,
  title: `Progress Episode ${n}`, feed_title: 'Progress Fixture', podcast_title: 'Progress Fixture',
  published_at: new Date(Date.UTC(2026, 9, n)).toISOString(), duration_seconds: 1800,
  position_seconds: position, completed: false, enclosure_url: `https://feeds.player-e2e.test/${n}.mp3`,
});

test('Continue listening opens all podcast episodes in progress', async ({ page }) => {
  await page.route('**/api/podcasts/episodes?*', route => {
    const filter = new URL(route.request().url()).searchParams.get('filter');
    const rows = filter === 'started' ? [episode(2, 754), episode(1, 30)] : [];
    return route.fulfill({ json: rows });
  });
  await page.goto('/login.html');
  await page.locator('#username').fill(ADMIN_USER);
  await page.locator('#password').fill(ADMIN_PASS);
  await page.getByRole('button', { name: 'Sign In' }).click();

  const banner = page.getByRole('region', { name: 'Continue listening' });
  await expect(banner).toBeVisible();
  await expect(banner).toContainText('Progress Episode 2');
  await banner.getByRole('button', { name: 'All in progress' }).click();
  await expect(banner).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'In progress' })).toHaveAttribute('aria-pressed', 'true');
  await expect(page.getByText('Progress Episode 1', { exact: true }).first()).toBeVisible();
  await expect(page.getByText('Progress Episode 2', { exact: true }).first()).toBeVisible();
});
