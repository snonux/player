import { currentMediaInfo, stopAndClose } from './player.js';
import { setEpisodeDeleted, episodeSnapshot, podcastSyncStatus } from './podcast-episode-state.js';
import { editLocalPodcast } from './local-podcast-state.js';
import { confirmEpisodeDelete, confirmLocalEpisodeDelete } from './podcast-metadata.js';
import { toast } from './utils.js';

// The player's Delete episode (in its controls menu) deletes the open podcast
// episode with the episode list's confirmation and deletion. Playback then
// moves on to the next episode; when there is none, the player stops.
// next() is the page's own "continue after this episode" step.
export function initPodcastPlayerDelete({ next, button = document.getElementById('btn-delete-episode') } = {}) {
  if (!button) return;
  const target = () => {
    const media = currentMediaInfo();
    if (media?.localPodcast && media.localPodcastEpisode) return { media, local: true, episode: media.localPodcastEpisode };
    if (media?.podcast_episode?.episode_key && podcastSyncStatus().account) return { media, local: false, episode: media.podcast_episode };
    return null;
  };
  const refresh = () => button.classList.toggle('hidden', !target());
  // player:media-opened fires before the player switches media.
  const later = () => setTimeout(refresh, 0);
  for (const event of ['player:media-opened', 'player:closed', 'player:position', 'podcast:state-changed', 'podcast:account-changed']) {
    document.addEventListener(event, later);
  }
  document.getElementById('btn-player-options')?.addEventListener('click', refresh);
  refresh();

  button.addEventListener('click', async () => {
    const current = target();
    if (!current) { refresh(); return; }
    const title = current.media.podcast?.title || current.media.title || current.media.file_name;
    if (!(current.local ? confirmLocalEpisodeDelete(title) : confirmEpisodeDelete(title))) return;
    // Continue first, while the feed order still lists this episode.
    try { await next?.(); } catch {}
    if (currentMediaInfo() === current.media) stopAndClose();
    if (current.local) {
      try { editLocalPodcast(current.episode, 'deletion', true); toast('Episode deleted'); }
      catch (error) { toast(error.message || 'Delete failed. Try again.', 'error'); }
      refresh();
      return;
    }
    try {
      await setEpisodeDeleted(current.episode);
      toast('Episode deleted from your podcast lists');
    } catch (error) {
      if (episodeSnapshot(current.episode).deletion?.deleted) {
        toast('Episode deleted on this browser · sync pending. Use Retry to sync it to your other devices.', 'info');
      } else toast(error.message || 'Delete failed. Try again.', 'error');
    }
    refresh();
  });
}
