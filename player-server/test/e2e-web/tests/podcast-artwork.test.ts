import { test, expect } from '@playwright/test';

const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4z8DwHwAFAAH/iZk9HQAAAABJRU5ErkJggg==', 'base64');
test('device podcast overview and episode fallback display publisher bitmaps', async ({ page }) => {
  const feed = 'https://cover.example/feed';
  const requests: Record<string, string>[] = [];
  await page.route('https://cover.example/**', async route => {
    requests.push(route.request().headers());
    const path = new URL(route.request().url()).pathname;
    if (path === '/feed') await route.fulfill({ contentType: 'application/rss+xml', headers: { 'Access-Control-Allow-Origin': '*' }, body:
      `<rss xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel><title>Cover podcast</title><image><url>https://cover.example/cover.png</url></image><item><guid>one</guid><title>Covered episode</title><itunes:image href="https://cover.example/missing.png"/><enclosure url="https://cover.example/audio.mp3"/></item></channel></rss>` });
    else if (path === '/cover.png') await route.fulfill({ contentType: 'image/png', body: png });
    else await route.fulfill({ status: 404 });
  });
  await page.goto('/index.html?local=podcasts');
  page.once('dialog', dialog => dialog.accept(feed));
  await page.getByRole('button', { name: 'Add feed URL', exact: true }).click();
  const cover = page.locator('img[data-podcast-artwork]');
  await expect(cover).toHaveCount(1);
  await expect.poll(() => cover.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(1);
  await page.getByRole('button', { name: 'Cover podcast', exact: true }).click();
  await expect(cover).toHaveAttribute('src', 'https://cover.example/cover.png');
  await expect.poll(() => cover.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(1);
  await page.getByRole('button', { name: 'Grid', exact: true }).click();
  await expect.poll(() => cover.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(1);
  for (const viewport of [{ width: 390, height: 844 }, { width: 844, height: 390 }]) {
    await page.setViewportSize(viewport);
    await expect(cover).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
  }
  for (const headers of requests) {
    expect(headers.authorization).toBeUndefined();
    expect(headers.cookie).toBeUndefined();
  }
});

test('account podcast overview displays its publisher cover', async ({ page }) => {
  await page.route('https://cover.example/account.png', route => route.fulfill({ contentType: 'image/png', body: png }));
  await page.goto('/login.html');
  await page.evaluate(async () => {
    document.body.innerHTML = '<main><div id="media-grid" class="media-grid"></div></main>';
    const { API } = await import('/js/api.js');
    const { state } = await import('/js/state.js');
    state.podcastOverview = true;
    API.podcasts = async () => [{ feed_key: 'feed:cover', title: 'Account podcast', image_url: 'https://cover.example/account.png' }];
    API.syncPodcast = async () => ({ epoch: 'cover', next_since: 1, has_more: false, subscriptions: [{ feed_key: 'feed:cover', title: 'Account podcast', subscribed: true }] });
    const { renderPodcastVirtualView } = await import('/js/podcast-views.js');
    await renderPodcastVirtualView(document.getElementById('media-grid'), { pageSize: 40 });
  });
  const cover = page.locator('img[data-podcast-artwork]');
  await expect.poll(() => cover.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(1);
  await expect(page.getByRole('button', { name: 'Podcast Account podcast', exact: true })).toBeVisible();
});
