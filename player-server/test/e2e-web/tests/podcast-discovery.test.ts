import { test, expect } from '@playwright/test';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { keyboardLogin } from './helpers/music-ui';

// Deterministic directory/publisher responses exercise the actual discovery UI
// and IndexedDB subscription workflow without writing to the shared server.
test('device podcast discovery browses, searches, subscribes and preserves manual URLs', async ({ page }) => {
  const feedURL = 'https://podcast-discovery.example/feed.xml';
  const manualURL = 'https://podcast-discovery.example/manual.xml';
  let requests = 0;
  await page.route('**/api/podcast-discovery?**', async route => {
    requests++;
    const popular = new URL(route.request().url()).searchParams.get('popular');
    await route.fulfill({ json: [{ title: popular ? 'Popular science' : 'Space science', author: 'Science publisher', feed_url: feedURL }] });
  });
  await page.route(feedURL, route => route.fulfill({
    contentType: 'application/rss+xml', headers: { 'Access-Control-Allow-Origin': '*' },
    body: '<?xml version="1.0"?><rss version="2.0"><channel><title>Space science</title><description>Science</description><item><title>First episode</title><guid>first</guid><enclosure url="https://podcast-discovery.example/audio.mp3" type="audio/mpeg"/></item></channel></rss>',
  }));
  await page.route(manualURL, route => route.fulfill({
    contentType: 'application/rss+xml', headers: { 'Access-Control-Allow-Origin': '*' },
    body: '<?xml version="1.0"?><rss version="2.0"><channel><title>Manual podcast</title><description>Manual RSS</description><item><title>Manual episode</title><guid>manual-first</guid><enclosure url="https://podcast-discovery.example/manual.mp3" type="audio/mpeg"/></item></channel></rss>',
  }));
  await page.goto('/index.html?local=podcasts');
  await page.getByRole('button', { name: 'Explore podcasts', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Explore podcasts', exact: true });
  await expect(dialog).toBeVisible();
  expect(requests).toBe(0);
  await dialog.getByRole('button', { name: 'Load popular podcasts', exact: true }).click();
  await expect(dialog.getByText('Popular science', { exact: true })).toBeVisible();
  await dialog.getByRole('searchbox', { name: 'Search podcasts' }).fill('space');
  await dialog.getByRole('button', { name: 'Search', exact: true }).click();
  await expect(dialog.getByText('Space science', { exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: 'Subscribe to Space science', exact: true }).click();
  await expect(dialog.getByRole('status')).toHaveText('Subscribed to Space science.');
  const addByURL = dialog.getByRole('button', { name: 'Add by feed URL', exact: true });
  await expect(addByURL).toHaveAttribute('aria-expanded', 'false');
  await addByURL.click();
  await expect(addByURL).toHaveAttribute('aria-expanded', 'true');
  await dialog.getByRole('textbox', { name: 'Feed URL', exact: true }).fill(manualURL);
  await dialog.getByRole('button', { name: 'Subscribe', exact: true }).click();
  await expect(dialog.getByRole('status')).toHaveText(`Subscribed to ${manualURL}.`);
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Space science', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Manual podcast', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Explore podcasts', exact: true })).toBeFocused();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Explore podcasts', exact: true }).click();
  await page.route('**/api/podcast-discovery?**', route => route.fulfill({ status: 502, json: { error: 'Podcast directory unavailable. Retry or add a feed URL.' } }));
  await dialog.getByRole('button', { name: 'Load popular podcasts', exact: true }).click();
  await expect(dialog.getByRole('status')).toContainText('Retry or add a feed URL');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
});

// Directory responses are deterministic; account login, subscription POSTs,
// their acknowledgements and catalog readback use the actual fixture server.
test('account discovery acknowledges directory and manual subscriptions using the real API', async ({ page }, testInfo) => {
  const directory = process.env.PLAYER_DISCOVERY_FIXTURE || process.env.PLAYER_PODCAST_FIXTURE;
  test.skip(!directory, 'Requires a marked disposable fixture for account mutations');
  expect(['.player-music-fixture', '.player-podcast-fixture'].some(marker => existsSync(join(directory!, marker)))).toBeTruthy();
  const fixture = JSON.parse(readFileSync(join(directory!, 'state.json'), 'utf8'));
  const auth = JSON.parse(readFileSync(fixture.auth_file || join(directory!, 'auth.json'), 'utf8'));
  // Firefox does not consistently expose fetch POST bodies on its network
  // response events. Observe the actual fetch and clone its real response;
  // never replace the request, credentials or response used by the app.
  await page.addInitScript(() => {
    const original = window.fetch.bind(window);
    (window as any).__podcastSubscriptionExchanges = [];
    (window as any).__podcastFeedRefreshes = [];
    window.fetch = async (resource, options) => {
      const response = await original(resource, options);
      if (/\/api\/(?:v1\/)?podcasts\/(?:sync|refresh)$/.test(String(resource))) {
        const request = JSON.parse(String(options?.body || '{}'));
        let result;
        try { result = await response.clone().json(); } catch { result = null; }
        const exchange = { request, status: response.status, result };
        if (request.subscriptions?.length) (window as any).__podcastSubscriptionExchanges.push(exchange);
        if (request.feed_key) (window as any).__podcastFeedRefreshes.push(exchange);
      }
      return response;
    };
  });
  await keyboardLogin(page, auth.user || auth.other || auth.admin);
  const initialSets = await page.request.get('/api/v1/sets');
  expect(initialSets.ok()).toBeTruthy();
  const visibleSetIDs = (await initialSets.json()).map((set: any) => set.id).sort();
  // New query identities isolate each browser/run while remaining actual RSS
  // URLs. The directory is the only intercepted endpoint in this journey.
  const run = `${testInfo.project.name}-${Date.now()}`;
  const publisher = new URL(process.env.PLAYER_DISCOVERY_FEED_URL || 'https://feeds.npr.org/510289/podcast.xml');
  const searchAddress = new URL(publisher);
  searchAddress.searchParams.set('player-discovery-regression', run);
  searchAddress.searchParams.set('mode', 'search');
  const searchURL = searchAddress.toString();
  const manualAddress = new URL(publisher);
  manualAddress.searchParams.set('player-discovery-regression', run);
  manualAddress.searchParams.set('mode', 'manual');
  // The public fixture's relay intentionally rejects non-canonical Host
  // headers. Shared identity vectors cover host/port normalization; actual
  // metadata requests here use the publisher's routable host spelling.
  const manualURL = `${manualAddress.toString()}#latest`;
  const searchKey = `feed:${publisher.host}${searchAddress.pathname}${searchAddress.search}`;
  const manualKey = `feed:${publisher.host}${manualAddress.pathname}${manualAddress.search}`;
  await page.route('**/api/podcast-discovery?**', route => route.fulfill({ json: [
    { title: 'Account discovery identity regression', author: 'NPR', feed_url: searchURL },
  ] }));
  const explore = page.getByRole('button', { name: 'Explore podcasts', exact: true });
  if (!await explore.isVisible()) await page.getByRole('button', { name: 'Toggle sets sidebar', exact: true }).click();
  await explore.click();
  const dialog = page.getByRole('dialog', { name: 'Explore podcasts', exact: true });
  await dialog.getByRole('searchbox', { name: 'Search podcasts' }).fill('science');
  await dialog.getByRole('button', { name: 'Search', exact: true }).click();

  async function assertAck(key: string) {
    const exchangeFor = (expected: string) => (window as any).__podcastSubscriptionExchanges
      .find((exchange: any) => exchange.request.subscriptions.some((edit: any) => edit.feed_key === expected && edit.subscribed));
    await expect.poll(() => page.evaluate(exchangeFor, key)).toBeTruthy();
    const exchange = await page.evaluate(exchangeFor, key);
    expect(exchange.status).toBe(200);
    const edit = exchange.request.subscriptions.find((entry: any) => entry.feed_key === key);
    expect(exchange.result.acked).toContain(edit.edit_id);
    expect((exchange.result.rejected || []).filter((entry: any) => entry.edit_id === edit.edit_id)).toEqual([]);
    const readback = await page.request.post('/api/v1/podcasts/sync', { data: { since: 0, limit: 500 } });
    expect(readback.ok()).toBeTruthy();
    expect((await readback.json()).subscriptions).toEqual(expect.arrayContaining([
      expect.objectContaining({ feed_key: key, subscribed: true }),
    ]));
  }
  async function assertRefreshed(key: string) {
    await expect.poll(() => page.evaluate(expected => (window as any).__podcastFeedRefreshes
      .find((exchange: any) => exchange.request.feed_key === expected && exchange.status === 200), key), { timeout: 30_000 }).toBeTruthy();
    const exchange = await page.evaluate(expected => (window as any).__podcastFeedRefreshes
      .find((entry: any) => entry.request.feed_key === expected && entry.status === 200), key);
    expect(['refreshed', 'fresh']).toContain(exchange.result.status);
    expect(exchange.result.feed_key).toBe(key);
    const episodes = await page.request.get(`/api/v1/podcasts/episodes?${new URLSearchParams({ feed_key: key, filter: 'all', limit: '100' })}`);
    expect(episodes.ok()).toBeTruthy();
    const rows = await episodes.json();
    expect(rows.length, 'a refreshed discovery feed must expose real publisher episodes to its subscriber').toBeGreaterThan(0);
    return rows[0].title as string;
  }
  await dialog.getByRole('button', { name: 'Subscribe to Account discovery identity regression', exact: true }).click();
  await assertAck(searchKey);
  const episodeTitle = await assertRefreshed(searchKey);
  await expect(dialog.getByRole('status')).toHaveText(/^(Subscribed|Subscription saved)/);

  const addByURL = dialog.getByRole('button', { name: 'Add by feed URL', exact: true });
  await expect(addByURL).toHaveAttribute('aria-expanded', 'false');
  await addByURL.click();
  await expect(addByURL).toHaveAttribute('aria-expanded', 'true');
  await dialog.getByRole('textbox', { name: 'Feed URL', exact: true }).fill(manualURL);
  await dialog.getByRole('button', { name: 'Subscribe', exact: true }).click();
  await assertAck(manualKey);
  await assertRefreshed(manualKey);
  const afterSubscribeSets = await page.request.get('/api/v1/sets');
  expect((await afterSubscribeSets.json()).map((set: any) => set.id).sort()).toEqual(visibleSetIDs);
  await expect(dialog.getByRole('status')).toHaveText(/^(Subscribed|Subscription saved)/);
  await page.keyboard.press('Escape');
  await page.reload();
  await expect.poll(async () => page.locator('#podcast-nav-list [data-feed-key]').evaluateAll(rows => rows.map(row => (row as HTMLElement).dataset.feedKey))).toEqual(expect.arrayContaining([searchKey, manualKey]));
  const feed = page.locator(`#podcast-nav-list [data-feed-key="${searchKey}"]`);
  if (!await feed.isVisible()) await page.getByRole('button', { name: 'Toggle sets sidebar', exact: true }).click();
  await feed.click();
  await expect(page.locator('.episode-card .title').filter({ hasText: episodeTitle }).first()).toBeVisible();
  const beforeRefresh = await page.evaluate(() => (window as any).__podcastFeedRefreshes.length);
  await page.getByRole('button', { name: 'Refresh feed', exact: true }).click();
  await expect.poll(() => page.evaluate(() => (window as any).__podcastFeedRefreshes.length)).toBeGreaterThan(beforeRefresh);
  await expect(page.locator('.episode-card .title').filter({ hasText: episodeTitle }).first()).toBeVisible();
});
