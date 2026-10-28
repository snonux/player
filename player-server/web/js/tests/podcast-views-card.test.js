import { episodeCardHtml } from '../podcast-views.js';

const failures = [];
function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}

console.log('Running podcast episode card compact layout tests...');

const base = {
  id: 7,
  title: 'Episode Seven',
  description: 'Long description that should not sit open by default.',
  feed_title: 'Feed',
  published_at: '2024-01-15T12:00:00Z',
  duration_seconds: 3600,
  position_seconds: 120,
  completed: false,
  server_copy: null,
};

{
  const html = episodeCardHtml(base, { showFeed: true });
  assert(html.includes('data-action="play"'), 'Play is primary');
  assert(html.includes('data-action="download-episode"'), 'Download is primary when missing');
  assert(html.includes('data-action="more"'), 'More hamburger is present');
  assert(html.includes('card-action-extra') && html.includes('data-action="complete"'),
    'completion is in More extras');
  assert(!html.includes('data-action="play-beginning"'), 'restart only when downloaded');
  assert(!html.includes('data-action="delete-download"'), 'delete only when downloaded');
  assert(html.includes('<details class="episode-description">'), 'description is expandable');
  assert(html.includes('<summary>Description</summary>'), 'description starts collapsed behind summary');
  assert(html.includes('Not downloaded'), 'status shows download state');
  assert(html.includes('In progress ·'), 'in-progress without server_copy (Android 1t3 parity)');
  assert(html.includes('Feed'), 'feed title when showFeed');
}

{
  const html = episodeCardHtml({
    ...base,
    server_copy: { media_id: 99 },
    completed: true,
  });
  assert(html.includes('aria-label="Resume episode"'), 'Resume when position and downloaded');
  assert(html.includes('Resume '), 'resume status when downloaded with position');
  assert(!html.includes('data-action="download-episode"'), 'no download button when present');
  assert(html.includes('data-action="play-beginning"') && html.includes('card-action-extra'),
    'restart lives in More');
  assert(html.includes('data-action="delete-download"') && html.includes('card-action-extra'),
    'delete lives in More');
  assert(html.includes('Mark uncompleted') && html.includes('card-action-extra'),
    'completion toggle lives in More');
  assert(html.includes('Downloaded on server'), 'status shows server copy');
}

{
  const html = episodeCardHtml({ ...base, server_copy: { media_id: 1 }, position_seconds: 0 });
  assert(html.includes('aria-label="Play episode"'), 'Play when no resume position');
  assert(!html.includes('In progress'), 'no in-progress without position');
}

{
  const html = episodeCardHtml(base, { downloading: true });
  assert(html.includes('data-action="cancel-download"'), 'Cancel while downloading');
  assert(!html.includes('data-action="play"'), 'Play hidden while downloading');
  assert(!html.includes('data-action="download-episode"'), 'Download hidden while downloading');
  assert(html.includes('Downloading…'), 'status shows transfer');
  const moreTag = html.match(/data-action="more"[^>]*>/)?.[0] || '';
  assert(moreTag && !/\sdisabled/.test(moreTag), 'More stays available during transfer');
  assert(html.includes('data-action="complete"') && html.includes('card-action-extra'),
    'completion stays reachable in More during transfer');
}

{
  const html = episodeCardHtml({ ...base, description: '  ' });
  assert(!html.includes('episode-description'), 'empty description omits details');
}

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
}
console.log('podcast episode card compact layout tests passed');
process.exit(0);
