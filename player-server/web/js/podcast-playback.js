import { API } from './api.js';
import { episodeSnapshot, reconcileEpisode, podcastSyncStatus } from './podcast-episode-state.js';

// Includes discovery in the three-second bound. The returned snapshot alone
// chooses startup. Late detail/sync work can update register storage, never a
// media element or the source object.
export async function preparePodcastPlayback(media, resumeFrom, timeoutMS = 3000) {
  const lease = podcastSyncStatus().generation;
  let episode = media.podcast_episode || null, classified = false, timer;
  const work = (async () => {
    let detail;
    try { detail = await API.mediaDetail(media.id); classified = true; } catch {}
    if (podcastSyncStatus().generation !== lease) return;
    episode = detail?.podcast_episode || episode;
    if (!episode && !resumeFrom) resumeFrom = detail?.progress?.finished ? 0 : (detail?.progress?.position_seconds ?? resumeFrom);
    if (episode) {
      episodeSnapshot(episode);
      await reconcileEpisode(episode, timeoutMS);
    }
  })().catch(() => {});
  try {
    await Promise.race([work, new Promise(resolve => { timer = setTimeout(resolve, timeoutMS); })]);
  } finally { clearTimeout(timer); }
  if (podcastSyncStatus().generation !== lease) return { cancelled: true };
  const snapshot = episode ? episodeSnapshot(episode) : null;
  return { episode: snapshot, account: podcastSyncStatus().account, lease, classified, position: snapshot?.position?.seconds ?? resumeFrom };
}

// An ended event after seeking exactly to the end is not natural playback.
// Compare against the native start/seek target; even <one sampling interval of
// actual forward playback may finish legitimately.
export function podcastNaturalEnd({ played, seeking, advancing, error, position, target }) {
  return played && !seeking && !error && advancing && position > target;
}
