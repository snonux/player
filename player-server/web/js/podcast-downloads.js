import { API } from './api.js';
import { podcastSyncStatus } from './podcast-episode-state.js';

// A transfer belongs to an account and catalog episode, not a rendered card.
// Rerendering or navigating back therefore keeps Cancel available.
const transfers = new Map();
function key(id) { return `${podcastSyncStatus().generation}:${id}`; }
function changed(id, media) { globalThis.document?.dispatchEvent?.(new CustomEvent('podcast:download-changed', { detail: { id, media } })); }
export function podcastDownloading(id) { return transfers.has(key(id)); }
export function cancelPodcastDownload(id) { transfers.get(key(id))?.controller.abort(); }
export function downloadPodcastEpisode(id) {
 const identity = key(id), previous = transfers.get(identity);
 if (previous) return previous.promise;
 const controller = new AbortController();
 const transfer = { controller, promise: null };
 transfers.set(identity, transfer);
 transfer.promise = Promise.resolve().then(() => API.downloadEpisode(id, { signal: controller.signal })).then(media => {
  if (identity === key(id)) changed(id, media);
  return media;
 }).finally(() => {
  if (transfers.get(identity) === transfer) transfers.delete(identity);
  changed(id);
 });
 changed(id);
 return transfer.promise;
}
globalThis.document?.addEventListener?.('podcast:account-changed', () => {
 for (const transfer of transfers.values()) transfer.controller.abort();
 transfers.clear();
});
