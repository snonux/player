import { observePodcastRetention, podcastRetentionExpired } from './podcast-retention.js';
const storageKey = ep => `player-local-podcast-state:v1:${encodeURIComponent(ep.feed_key)}:${encodeURIComponent(ep.episode_key)}`;
const changed = () => globalThis.document?.dispatchEvent(new Event('local-podcast:state-changed'));

export function localPodcastSnapshot(ep, now = Date.now()) {
  const row = JSON.parse(localStorage.getItem(storageKey(ep)) || '{}');
  const marker = observePodcastRetention('device', { ...ep, ...row }, now);
  if (podcastRetentionExpired(marker, now) && !row.deletion?.deleted) {
    row.deletion = { deleted: true, observed_ms: now };
    localStorage.setItem(storageKey(ep), JSON.stringify(row));
  }
  return row;
}

export function editLocalPodcast(ep, name, value, now = Date.now()) {
  const row = localPodcastSnapshot(ep, now);
  const ms = Math.max(now, (row[name]?.observed_ms || 0) + 1);
  if (name === 'completion') row.completion = { completed: value, observed_ms: ms };
  else if (name === 'deletion') row.deletion = { deleted: value, observed_ms: ms };
  else if (name === 'position') row.position = { observed_ms: ms };
  else throw Error('Unknown podcast edit');
  localStorage.setItem(storageKey(ep), JSON.stringify(row));
  observePodcastRetention('device', { ...ep, ...row }, now);
  changed();
  return row;
}
