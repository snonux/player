// Async resume reads from either sequencing mode share one source intent.
// Browsing may cancel a Sets read separately without canceling album playback.
let revision = 0;
export function beginPlaybackIntent() { return ++revision; }
export function currentPlaybackIntent() { return revision; }
export function isPlaybackIntent(value) { return value === revision; }
