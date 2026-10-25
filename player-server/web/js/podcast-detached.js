import { podcastSyncStatus } from './podcast-episode-state.js';
import { preparePodcastPlayback } from './podcast-playback.js';

// Account identity travels between windows; generation leases never do.
export function transferPodcastMedia(media) {
  const account = podcastSyncStatus().account;
  if (media.podcast_account !== undefined && media.podcast_account !== account) return null;
  const copy = { ...media, podcast_account: account };
  delete copy.podcast_lease;
  delete copy.podcast_prepared;
  return copy;
}

// One deadline includes authenticated account discovery and scoped reconcile.
// An unknown account may play its transferred local snapshot, but receives a
// null lease so a later sign-in cannot silently authorize its progress writes.
export async function prepareDetachedPlayback(payload, { accountReady, onAccount, timeoutMS = 3000 } = {}) {
  const deadline = Date.now() + timeoutMS;
  const owner = podcastSyncStatus().generation;
  let timer;
  let cfg;
  try {
    cfg = await Promise.race([Promise.resolve(accountReady).catch(() => null),
      new Promise(resolve => { timer = setTimeout(() => resolve(null), timeoutMS); })]);
  } finally { clearTimeout(timer); }
  const media = { ...payload.media };
  const position = Number(payload.resumeFrom || 0);
  const current = podcastSyncStatus();
  if (!cfg?.user_id) {
    if (current.generation !== owner || (current.account && media.podcast_account !== undefined && media.podcast_account !== current.account)) return { cancelled: true };
    if (cfg && 'user_id' in cfg) onAccount?.(cfg);
    return { media: { ...media, podcast_lease: null, podcast_prepared: true }, position, authenticated: false };
  }
  if (current.generation !== owner && current.account !== cfg.user_id) return { cancelled: true };
  onAccount?.(cfg);
  if (media.podcast_account !== undefined && media.podcast_account !== cfg.user_id) return { cancelled: true };
  const remaining = Math.max(0, deadline - Date.now());
  const prepared = remaining > 0 ? await preparePodcastPlayback(media, position, remaining) : {
    episode: media.podcast_episode, position, lease: podcastSyncStatus().generation };
  if (prepared.cancelled) return prepared;
  return { media: { ...media, podcast_episode: prepared.episode, podcast_account: cfg.user_id,
    podcast_lease: prepared.lease, podcast_classified: prepared.classified, podcast_prepared: true },
    position: media.podcast_from_beginning ? 0 : prepared.position, authenticated: true };
}
