// @ts-nocheck
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { test, expect } from '@playwright/test';
import { keyboardLogin } from './helpers/music-ui';
const dir = process.env.PLAYER_PODCAST_FIXTURE;
test.skip(!dir, 'Requires disposable podcast fixture');
const auth = dir ? JSON.parse(readFileSync(`${dir}/auth.json`, 'utf8')) : {};
test.beforeEach(async ({ page, baseURL }) => {
  await page.route('**/js/**', async route => {
    const url = new URL(route.request().url());
    if (url.origin !== new URL(baseURL).origin || !url.pathname.startsWith('/js/')) return route.continue();
    const path = url.pathname;
    await route.fulfill({ path: resolve(__dirname, '../../../web', path.slice(1)), contentType: 'application/javascript' });
  });
  await keyboardLogin(page, auth.admin);
});
test('offline retention boundary persists tombstones and isolates downloaded bytes', async ({ page }) => {
  const result = await page.evaluate(async () => {
    const state = await import('/js/podcast-episode-state.js');
    const retention = await import('/js/podcast-retention.js');
    const cacheModule = await import('/js/playback-cache.js');
    const base = Date.now(), week = retention.podcastRetentionMS;
    const ep = { feed_key: 'feed:browser-retention', episode_key: 'one' };
    const guarded = { ...ep, episode_key: 'guarded' };
    let marker = retention.observePodcastRetention(70003, { ...guarded, completion: { completed: true, observed_ms: base - week * 3 } }, base);
    marker = retention.observePodcastRetention(70003, { ...guarded, completion: { completed: true, observed_ms: base - week * 3 } }, base + week - 1);
    const repeatedGrace = marker.seen_ms === base;
    marker = retention.observePodcastRetention(70003, { ...guarded, position: { observed_ms: base } }, base);
    const replayProtected = !retention.podcastRetentionExpired(marker, base + week);
    marker = retention.observePodcastRetention(70003, { ...guarded, completion: { completed: true, observed_ms: base + 1 } }, base);
    marker = retention.observePodcastRetention(70003, { ...guarded, deletion: { deleted: false, observed_ms: base + 2 } }, base);
    const restoreProtected = !retention.podcastRetentionExpired(marker, base + week);
    state.setPodcastAccount(70001); state.completionEdit(ep, true, base);
    state.sweepPodcastRetention(base + week - 1);
    const before = state.episodeSnapshot(ep, base + week - 1).deletion;
    state.sweepPodcastRetention(base + week);
    const after = state.episodeSnapshot(ep, base + week).deletion;
    state.setPodcastAccount(70002); const other = state.episodeSnapshot(ep).deletion;
    state.setPodcastAccount(70001); const reload = state.episodeSnapshot(ep, base + week).deletion;
    const noAutoPending = state.podcastSyncStatus().pending === 0;
    state.applyPodcastPull({ episodes: [{ ...ep, position: { seconds: 1, observed_ms: base + 1 } }] });
    const remoteReplayUnhides = state.episodeSnapshot(ep, base + week).deletion === null;
    state.setPodcastAccount(70004); state.completionEdit(ep, true, base);
    state.applyPodcastPull({ episodes: [{ ...ep, deletion: { deleted: false, observed_ms: base + 1 } }] });
    const remoteRestoreUnhides = state.episodeSnapshot(ep, base + week).deletion?.deleted === false;
    state.setPodcastAccount(70001);
    const url = `${location.origin}/retention-test-copy`;
    for (const account of [70001, 70002]) {
      const name = `player-playback-v1-${account}`;
      await (await caches.open(name)).put(url, new Response('podcast bytes'));
      localStorage.setItem(`${name}:index`, JSON.stringify({ [url]: { url, media: { id: 90001, type: 'audio', podcast_episode: ep }, size: 13, used: base } }));
    }
    cacheModule.setPlaybackCacheAccount(70001);
    const originalFetch = window.fetch;
    window.fetch = async () => { throw new TypeError('offline fixture'); };
    try { await cacheModule.playbackCacheSource({ id: 90001, type: 'audio', is_podcast: true }, url); }
    finally { window.fetch = originalFetch; }
    await cacheModule.removeExpiredPodcastCache(70001, ep);
    const activeBytes = Boolean(await (await caches.open('player-playback-v1-70001')).match(url));
    cacheModule.releasePlaybackCache();
    await cacheModule.removeExpiredPodcastCache(70001, ep, () => false);
    const staleBytes = Boolean(await (await caches.open('player-playback-v1-70001')).match(url));
    await cacheModule.removeExpiredPodcastCache(70001, ep);
    return { noAutoPending, remoteReplayUnhides, remoteRestoreUnhides, repeatedGrace, replayProtected, restoreProtected, activeBytes, staleBytes, before, after, other, reload,
      ownBytes: Boolean(await (await caches.open('player-playback-v1-70001')).match(url)),
      otherBytes: Boolean(await (await caches.open('player-playback-v1-70002')).match(url)) };
  });
  expect(result.noAutoPending).toBe(true); expect(result.remoteReplayUnhides).toBe(true); expect(result.remoteRestoreUnhides).toBe(true);
  expect(result.repeatedGrace).toBe(true); expect(result.replayProtected).toBe(true); expect(result.restoreProtected).toBe(true);
  expect(result.before).toBeNull(); expect(result.after.deleted).toBe(true);
  expect(result.other).toBeNull(); expect(result.reload.deleted).toBe(true);
  expect(result.activeBytes).toBe(true); expect(result.staleBytes).toBe(true);
  expect(result.ownBytes).toBe(false); expect(result.otherBytes).toBe(true);
});
test('device feed manual completion and seven-day expiry survive refresh', async ({ page }) => {
  await page.context().clearCookies();
  await page.goto('/index.html?local=podcasts');
  await page.evaluate(async () => {
    const store = await import('/js/local-podcasts-store.js');
    const feed_key = 'feed:https://example.com/retention.xml';
    await store.saveLocalPodcastFeed({ feed_key, feed_url: 'https://example.com/retention.xml', title: 'Retention device feed', episodes: [
      { feed_key, episode_key: `${feed_key}|one`, title: 'Device retention episode', enclosure_url: 'https://example.com/one.mp3' },
      { feed_key, episode_key: `${feed_key}|two`, title: 'Manual delete episode', enclosure_url: 'https://example.com/two.mp3' },
    ] });
  });
  await page.reload(); await page.getByRole('button', { name: 'Retention device feed', exact: true }).click();
  await page.locator('summary[aria-label="More for Manual delete episode"]').click();
  page.once('dialog', dialog => dialog.accept());
  await page.getByRole('button', { name: 'Delete episode', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Manual delete episode', exact: true })).toHaveCount(0);
  await page.locator('summary[aria-label="More for Device retention episode"]').click();
  await page.getByRole('button', { name: 'Mark completed', exact: true }).click();
  await page.getByRole('button', { name: 'Completed', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Device retention episode', exact: true })).toBeVisible();
  await page.clock.install(); await page.clock.fastForward(7 * 86400000);
  await page.getByRole('button', { name: 'All', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Device retention episode', exact: true })).toHaveCount(0);
  await page.reload(); await page.getByRole('button', { name: 'Retention device feed', exact: true }).click();
  await page.getByRole('button', { name: 'All', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Device retention episode', exact: true })).toHaveCount(0);
});
test('device feed natural playback completes while seeking to end does not', async ({ page }) => {
  await page.context().clearCookies();
  // A small real PCM source makes this listening check independent of publisher
  // state and codecs. No fake ended events or media-element clock mutation.
  const samples = 16000 * 2, wav = Buffer.alloc(44 + samples * 2);
  wav.write('RIFF', 0); wav.writeUInt32LE(wav.length - 8, 4); wav.write('WAVEfmt ', 8);
  wav.writeUInt32LE(16, 16); wav.writeUInt16LE(1, 20); wav.writeUInt16LE(1, 22);
  wav.writeUInt32LE(16000, 24); wav.writeUInt32LE(32000, 28); wav.writeUInt16LE(2, 32); wav.writeUInt16LE(16, 34);
  wav.write('data', 36); wav.writeUInt32LE(samples * 2, 40);
  for (let n = 0; n < samples; n++) wav.writeInt16LE(Math.round(Math.sin(n / 16000 * Math.PI * 440 * 2) * 1500), 44 + n * 2);
  await page.route('**/retention-fixture.wav', route => route.fulfill({ body: wav, contentType: 'audio/wav' }));
  await page.goto('/index.html?local=podcasts');
  await page.evaluate(async () => {
    const store = await import('/js/local-podcasts-store.js');
    const feed_key = 'feed:https://example.com/natural.xml';
    await store.saveLocalPodcastFeed({ feed_key, feed_url: 'https://example.com/natural.xml', title: 'Natural device feed', episodes: [
      { feed_key, episode_key: `${feed_key}|one`, title: 'Natural device episode', enclosure_url: `${location.origin}/retention-fixture.wav` },
      { feed_key, episode_key: `${feed_key}|two`, title: 'Natural successor episode', enclosure_url: `${location.origin}/retention-fixture.wav` },
    ] });
  });
  await page.reload(); await page.getByRole('button', { name: 'Natural device feed', exact: true }).click();
  await page.getByRole('button', { name: 'Natural device episode', exact: true }).click();
  await expect.poll(() => page.locator('#media-audio').evaluate(a => !a.paused && a.currentTime > 0.1)).toBe(true);
  await expect(page.locator('#player')).toContainText('Natural successor episode');
  await expect(page.getByRole('button', { name: 'Natural device episode', exact: true })).toHaveCount(0);
  await expect.poll(() => page.locator('#media-audio').evaluate(a => a.ended)).toBe(true);
  await expect(page.locator('#player')).toContainText('Natural successor episode');
  await page.getByRole('button', { name: 'More player controls', exact: true }).click();
  await page.locator('#btn-minimize').click();
  await page.getByRole('button', { name: 'Completed', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Natural device episode', exact: true })).toBeVisible();
  await page.locator('summary[aria-label="More for Natural device episode"]').click();
  await page.getByRole('button', { name: 'Mark uncompleted', exact: true }).click();
  await page.getByRole('button', { name: 'Uncompleted', exact: true }).click();
  await page.getByRole('button', { name: 'Natural device episode', exact: true }).click();
  await expect.poll(() => page.locator('#media-audio').evaluate(a => !a.paused && a.currentTime > 0.1)).toBe(true);
  await page.getByRole('button', { name: 'Restore player', exact: true }).click();
  await page.locator('#btn-play').click();
  await page.locator('#media-audio').evaluate(a => { a.currentTime = a.duration; });
  await page.getByRole('button', { name: 'More player controls', exact: true }).click();
  await page.locator('#btn-minimize').click();
  await page.getByRole('button', { name: 'Completed', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Natural device episode', exact: true })).toHaveCount(0);
});
test('retention cleanup revalidates completion and guards after held cache work', async ({ page }) => {
  const results = await page.evaluate(async () => {
    const state = await import('/js/podcast-episode-state.js');
    const retention = await import('/js/podcast-retention.js');
    const playback = await import('/js/playback-cache.js');
    const base = Date.now(), clock = base - retention.podcastRetentionMS * 2;
    const results = [];
    for (const [index, mutation] of ['replay', 'restore', 'recomplete', 'uncomplete', 'same-clock-uncomplete', 'normal', 'alias', 'held-delete-replay'].entries()) {
      const account = 71000 + index, name = `player-playback-v1-${account}`;
      const ep = { feed_key: 'feed:cleanup-race', episode_key: mutation, server_copy: { media_id: 98000 + index },
        completion: { completed: true, source: 'manual', observed_ms: clock } };
      state.setPodcastAccount(account); playback.setPlaybackCacheAccount(account);
      const originalOpen = caches.open.bind(caches), cache = await originalOpen(name);
      const url = `${location.origin}/cleanup-race-${index}`;
      const cachedEpisode = mutation === 'alias' ? { ...ep, episode_key: 'old-alias' } : ep;
      await cache.put(url, new Response('retained bytes'));
      const entry = { url, media: { id: ep.server_copy.media_id, type: 'audio', is_podcast: true, podcast_episode: cachedEpisode }, size: 14, used: base };
      localStorage.setItem(`${name}:index`, JSON.stringify({ [url]: entry }));
      retention.observePodcastRetention(account, ep, base - retention.podcastRetentionMS);
      state.episodeSnapshot(ep);
      let unblock, entered;
      const blocked = new Promise(resolve => { entered = resolve; });
      const release = new Promise(resolve => { unblock = resolve; });
      let armed = true;
      const originalDelete = cache.delete.bind(cache);
      if (mutation === 'held-delete-replay') cache.delete = async key => {
        const result = await originalDelete(key);
        if (armed) { armed = false; entered(); await release; }
        return result;
      };
      caches.open = async cacheName => {
        if (cacheName === name && armed && mutation !== 'held-delete-replay') {
          armed = false; entered(); await release;
        }
        return cacheName === name ? cache : originalOpen(cacheName);
      };
      try {
        const cleaning = state.sweepPodcastRetention(base);
        await blocked;
        if (mutation === 'replay' || mutation === 'held-delete-replay') state.applyPodcastPull({ episodes: [{ ...ep, position: { seconds: 1, observed_ms: clock + 1 } }] });
        else if (mutation === 'restore') state.applyPodcastPull({ episodes: [{ ...ep, deletion: { deleted: false, observed_ms: clock + 1 } }] });
        else if (mutation === 'recomplete') state.applyPodcastPull({ episodes: [{ ...ep, completion: { completed: true, source: 'manual', observed_ms: clock + 1 } }] });
        else if (mutation === 'uncomplete') state.applyPodcastPull({ episodes: [{ ...ep, completion: { completed: false, source: 'manual', observed_ms: clock + 1 } }] });
        else if (mutation === 'same-clock-uncomplete') state.applyPodcastPull({ episodes: [{ ...ep, completion: { completed: false, source: 'manual', observed_ms: clock } }] });
        unblock(); await cleaning;
      } finally { unblock(); caches.open = originalOpen; }
      results.push({ mutation, bytes: Boolean(await cache.match(url)), indexed: Boolean(JSON.parse(localStorage.getItem(`${name}:index`))[url]) });
    }
    return results;
  });
  for (const row of results) {
    const removed = row.mutation === 'normal' || row.mutation === 'alias';
    expect(row.bytes, row.mutation).toBe(!removed);
    expect(row.indexed, row.mutation).toBe(!removed);
  }
});
test('sparse completion after account reload preserves replay and restore cache guards', async ({ page }) => {
  const results = await page.evaluate(async () => {
    const state = await import('/js/podcast-episode-state.js');
    const retention = await import('/js/podcast-retention.js');
    const playback = await import('/js/playback-cache.js');
    const base = Date.now(), week = retention.podcastRetentionMS, results = [];
    for (const [index, guard] of ['position', 'deletion'].entries()) {
      const account = 72000 + index * 2, name = `player-playback-v1-${account}`;
      const ep = { feed_key: 'feed:sparse-guard', episode_key: guard };
      state.setPodcastAccount(account); playback.setPlaybackCacheAccount(account);
      const cache = await caches.open(name), url = `${location.origin}/sparse-guard-${guard}`;
      await cache.put(url, new Response('guarded episode'));
      localStorage.setItem(`${name}:index`, JSON.stringify({ [url]: { url, media: { id: 97000 + index, type: 'audio', is_podcast: true, podcast_episode: { ...ep, episode_key: 'old-physical-key' } }, size: 15, used: base } }));
      state.episodeSnapshot({ ...ep, server_copy: { media_id: 97000 + index }, completion: { completed: true, observed_ms: 100 },
        [guard]: guard === 'position' ? { seconds: 1, observed_ms: 300 } : { deleted: false, observed_ms: 300 } });
      state.setPodcastAccount(account + 1); state.setPodcastAccount(account);
      state.applyPodcastPull({ episodes: [{ ...ep, completion: { completed: true, observed_ms: 200 } }], has_more: true });
      let marker = retention.readPodcastRetention(account)[0];
      await state.sweepPodcastRetention(marker.seen_ms + week);
      const guardedBytes = Boolean(await cache.match(url));
      const guardedIndex = Boolean(JSON.parse(localStorage.getItem(`${name}:index`))[url]);
      const visible = state.episodeSnapshot(ep, marker.seen_ms + week).deletion === null;
      state.setPodcastAccount(account + 1); state.setPodcastAccount(account);
      const afterInterruptedPull = state.episodeSnapshot(ep, marker.seen_ms + week).deletion === null;
      state.applyPodcastPull({ episodes: [{ ...ep, completion: { completed: true, observed_ms: 400 } }] });
      marker = retention.readPodcastRetention(account)[0];
      const beforeBoundary = state.episodeSnapshot(ep, marker.seen_ms + week - 1).deletion === null;
      await state.sweepPodcastRetention(marker.seen_ms + week);
      results.push({ guard, guardedBytes, guardedIndex, visible, afterInterruptedPull, beforeBoundary,
        finalBytes: Boolean(await cache.match(url)), finalIndex: Boolean(JSON.parse(localStorage.getItem(`${name}:index`))[url]) });
    }
    return results;
  });
  for (const row of results) {
    expect(row.guardedBytes, row.guard).toBe(true); expect(row.guardedIndex, row.guard).toBe(true);
    expect(row.visible, row.guard).toBe(true); expect(row.afterInterruptedPull, row.guard).toBe(true);
    expect(row.beforeBoundary, row.guard).toBe(true);
    expect(row.finalBytes, row.guard).toBe(false); expect(row.finalIndex, row.guard).toBe(false);
  }
});
