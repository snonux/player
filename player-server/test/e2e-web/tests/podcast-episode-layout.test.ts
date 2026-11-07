import { test, expect } from '@playwright/test';

test('subscribed episode layouts retain cards, focus and actions', async ({ page }) => {
  await page.goto('/login.html');
  await page.evaluate(async () => {
    document.body.innerHTML = '<main class="main-content"><div id="media-grid" class="media-grid picture-grid"></div></main>';
    document.querySelector('link[href="/css/login.css"]')?.remove();
    for (const name of ['theme', 'components', 'layout']) {
      const css = document.createElement('link'); css.rel = 'stylesheet'; css.href = `/css/${name}.css`; document.head.append(css);
      await new Promise(resolve => { css.onload = resolve; css.onerror = resolve; });
    }
    const { initKeyboardAccess } = await import('/js/keyboard-access.js'); initKeyboardAccess();
    const { API } = await import('/js/api.js');
    const { state } = await import('/js/state.js');
    state.podcastFeedKey = ''; state.podcastOverview = false;
    API.subscribedEpisodes = async () => Array.from({ length: 4 }, (_, i) => ({
      id: i + 1, feed_key: 'feed:test', episode_key: `guid:${i}`, title: `Episode ${i} ${'Long readable title '.repeat(6)}`,
      feed_title: 'A podcast', description: '<p>Readable show notes</p>', published_at: '2026-10-30', duration_seconds: 600, position_seconds: 60,
    }));
    const { renderPodcastVirtualView } = await import('/js/podcast-views.js');
    await renderPodcastVirtualView(document.getElementById('media-grid'), { pageSize: 40 });
    const selection = await import('/js/selection.js'); selection.initSelection(); selection.select(1);
  });
  const layout = page.getByRole('group', { name: 'Episode layout' });
  const cards = page.locator('.episode-card');
  await expect(cards).toHaveCount(4);
  expect(new Set(await cards.evaluateAll(rows => rows.map(row => row.getBoundingClientRect().x)))).toHaveProperty('size', 4);
  await layout.getByRole('button', { name: 'List', exact: true }).focus();
  await page.keyboard.press('Enter');
  await expect(layout.getByRole('button', { name: 'List', exact: true })).toBeFocused();
  await expect(layout.getByRole('button', { name: 'List', exact: true })).toHaveAttribute('aria-pressed', 'true');
  expect(new Set(await cards.evaluateAll(rows => rows.map(row => row.getBoundingClientRect().x)))).toHaveProperty('size', 1);
  await expect(cards.nth(1)).toHaveClass(/selected/);
  expect(await cards.first().evaluate(card => getComputedStyle(card).gridTemplateColumns.split(' ').length)).toBe(2);
  expect((await layout.getByRole('button', {name:'Grid',exact:true}).boundingBox())!.height).toBeGreaterThanOrEqual(44);
  await cards.first().getByRole('button', { name: 'More episode actions' }).click();
  await expect(page.getByRole('group', {name:'Media actions',exact:true}).getByRole('button', { name: 'Mark completed', exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  await page.addStyleTag({content:'html { font-size: 160%; }'});
  for (const viewport of [{width:390,height:844}, {width:844,height:390}]) {
    await page.setViewportSize(viewport);
    await layout.getByRole('button', { name: 'Grid', exact: true }).click();
    await cards.first().locator('summary').click();
    await expect(cards.first()).toContainText('Readable show notes');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
    await cards.first().locator('summary').click();
    await layout.getByRole('button', { name: 'List', exact: true }).click();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
  }
});

test('device podcast grid preserves cached episodes and playback controls', async ({ page }) => {
  const feed = 'https://grid.example/feed';
  await page.route(feed, route => route.fulfill({ contentType: 'application/rss+xml', headers: { 'Access-Control-Allow-Origin': '*' }, body:
    `<rss><channel><title>Grid podcast</title>${Array.from({length:4},(_,i)=>`<item><guid>${i}</guid><title>Device episode ${i}</title><description>Device notes</description><enclosure url="https://grid.example/${i}.mp3"/></item>`).join('')}</channel></rss>` }));
  await page.goto('/index.html?local=podcasts');
  page.once('dialog', dialog => dialog.accept(feed));
  await page.getByRole('button', {name:'Add feed URL',exact:true}).click();
  await page.getByRole('button', {name:'Grid podcast',exact:true}).click();
  const layout = page.getByRole('group', {name:'Episode layout'});
  await layout.getByRole('button', {name:'Grid',exact:true}).click();
  await expect(page.locator('.episode-grid .local-files-row')).toHaveCount(4);
  const cards = page.locator('.episode-grid .local-files-row');
  const first = await cards.nth(0).boundingBox();
  const second = await cards.nth(1).boundingBox();
  expect(second!.x).toBeGreaterThan(first!.x);
  for (const viewport of [{width:390,height:844}, {width:844,height:390}]) {
    await page.setViewportSize(viewport);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
    await expect(page.getByRole('button',{name:'Device episode 0',exact:true})).toBeVisible();
  }
  await expect(page.getByRole('button',{name:'Device episode 0',exact:true})).toBeVisible();
  await page.context().setOffline(true);
  await layout.getByRole('button', {name:'List',exact:true}).click();
  await expect(page.locator('.episode-grid')).toHaveCount(0);
  await expect(page.getByRole('button',{name:'Device episode 0',exact:true})).toBeVisible();
});
