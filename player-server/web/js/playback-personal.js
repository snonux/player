import { API, NO_NOTE } from './api.js';
import { open as openNotes } from './notes.js';
import { initBookmarks } from './bookmarks.js';
import { applyFavoriteButton } from './views/media-actions.js';
import { toast } from './utils.js';

// Only library and authenticated detached players initialize personal actions.
export function initPlaybackPersonal({ getMedia, getElement, available }) {
  const bookmarks = initBookmarks({ getMedia, getElement, available });
  const notes = document.getElementById('btn-player-notes');
  const favorite = document.getElementById('btn-player-favorite');
  let epoch = 0;
  notes?.addEventListener('click', async () => {
    const media = getMedia();
    if (!media || !available()) return;
    const token = ++epoch;
    try {
      const note = await API.notes(media.id);
      if (token !== epoch || getMedia()?.id !== media.id || !available()) return;
      if (note !== NO_NOTE && typeof note?.content !== 'string') throw new Error('Invalid note response');
      openNotes(media.id, note === NO_NOTE ? '' : note.content);
    } catch {
      if (token === epoch) toast('Could not load note. Try again.', 'error');
    }
  });
  let favoritePending = false;
  favorite?.addEventListener('click', async () => {
    const media = getMedia();
    if (!media || !available() || favoritePending) return;
    favoritePending = true;
    favorite.disabled = true;
    try {
      const result = await API.favorite(media.id);
      if (getMedia()?.id !== media.id) return;
      applyFavoriteButton(favorite, result.favorite);
      toast(result.favorite ? 'Added to favorites' : 'Removed from favorites');
    } catch { toast('Could not update favorite. Try again.', 'error'); }
    finally { favoritePending = false; favorite.disabled = false; }
  });
  return { setMedia(media) {
    epoch++;
    bookmarks?.setMedia(media);
    notes?.classList.toggle('hidden', !media);
    favorite?.classList.toggle('hidden', !media);
    applyFavoriteButton(favorite, Boolean(media?.is_favorite || media?.favorite));
  } };
}
