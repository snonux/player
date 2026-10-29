import { episodeSetFolderCardHtml } from '../podcasts.js';

const failures = [];
function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}

console.log('Running set-folder podcast episode card compact layout tests...');

const base = {
  id: 3,
  title: 'Set Folder Episode',
  description: 'Full episode notes that stay collapsed.',
  published_at: '2024-02-01T08:00:00Z',
  duration_seconds: 1800,
  position_seconds: 90,
  is_completed: false,
  is_downloaded: false,
};

{
  const html = episodeSetFolderCardHtml(base);
  assert(html.includes('data-action="play"'), 'Play is primary');
  assert(html.includes('data-action="download-episode"'), 'Download is primary when missing');
  assert(html.includes('data-action="more"'), 'More hamburger is present');
  assert(html.includes('card-action-extra') && html.includes('data-action="complete"'),
    'completion is in More extras');
  assert(!html.includes('data-action="play-beginning"'), 'restart only when downloaded');
  assert(!html.includes('data-action="delete-download"'), 'delete only when downloaded');
  assert(html.includes('<details class="episode-description">'), 'description is expandable');
  assert(html.includes('<summary>Show notes</summary>'), 'description starts collapsed');
  assert(html.includes('Not downloaded'), 'status shows download state');
  assert(html.includes('In progress ·'), 'in-progress without download (Android 1t3 parity)');
  assert(!html.includes('Full episode notes that stay collapsed.</div>'),
    'description is not open subtitle');
}

{
  const html = episodeSetFolderCardHtml({
    ...base,
    is_downloaded: true,
    is_completed: true,
  });
  assert(html.includes('aria-label="Resume episode"'), 'Resume when position and downloaded');
  assert(html.includes('Resume '), 'resume status when downloaded with position');
  assert(!html.includes('data-action="download-episode"'), 'no download when present');
  assert(html.includes('data-action="play-beginning"') && html.includes('card-action-extra'),
    'restart lives in More');
  assert(html.includes('data-action="delete-download"') && html.includes('card-action-extra'),
    'delete lives in More');
  assert(html.includes('Mark uncompleted') && html.includes('card-action-extra'),
    'completion toggle lives in More');
  assert(html.includes('Downloaded on server'), 'status shows downloaded');
}

{
  const html = episodeSetFolderCardHtml({ ...base, is_downloaded: true, position_seconds: 0 });
  assert(html.includes('aria-label="Play episode"'), 'Play when no resume position');
}

{
  const html = episodeSetFolderCardHtml(base, { downloading: true });
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
  const html = episodeSetFolderCardHtml({ ...base, description: '  ', position_seconds: 0 });
  assert(!html.includes('episode-description'), 'empty description omits details');
  assert(!html.includes('In progress'), 'no in-progress without position');
}

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
}
console.log('set-folder podcast episode card compact layout tests passed');
process.exit(0);
