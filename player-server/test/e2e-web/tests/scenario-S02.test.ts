import { test, expect, admin, podcast } from './helpers/scenario-fixture';
test('S02: podcast subscription downloads real audio and records completion', async ({ sandbox }) => {
  const { cookie, api } = await admin();
  await podcast(cookie, async feed => {
    expect(await api('GET', '/podcasts')).toEqual(expect.arrayContaining([expect.objectContaining({ id: feed.id, feed_url: feed.feed_url })]));
    const episodes = await api('GET', `/podcasts/${feed.set_id}/episodes`);
    expect(episodes).toHaveLength(2);
    const episode = episodes[0];
    expect(episode.title).toBeTruthy();
    expect(episode.episode_url).toMatch(/\.mp3$/);
    const media = await api('POST', `/podcasts/episodes/${episode.id}/download`);
    expect(media.id).toBeGreaterThan(0);
    expect(media.file_name).toBeTruthy();
    expect(media.duration).toBeGreaterThan(0);
    expect(await api('GET', `/podcasts/${feed.set_id}/episodes`)).toEqual(expect.arrayContaining([expect.objectContaining({ id: episode.id, is_downloaded: true })]));
    // Crossing the actual playback threshold is required; a one-shot seek is not listening.
    for (const position_seconds of [12, 24, 36, 48, 60, 72, 120]) await api('POST', '/progress', { media_id: media.id, position_seconds });
    expect(await api('GET', '/in-progress')).toEqual(expect.arrayContaining([expect.objectContaining({ id: media.id })]));
    const { call } = await import('./helpers/scenario-http');
    expect((await call('POST', `/api/v1/podcasts/episodes/${episode.id}/complete`, { cookie })).status).toBe(204);
    expect(await api('GET', `/podcasts/${feed.set_id}/episodes`)).toEqual(expect.arrayContaining([expect.objectContaining({ id: episode.id, is_completed: true })]));
    expect(sandbox.sql('SELECT count(*) FROM podcast_status WHERE is_completed=1')).toBe('1');
  });
});
