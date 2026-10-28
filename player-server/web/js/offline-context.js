// Explicit local-download / on-device modes are separate from authenticated server identity.
// They are never persisted as an online user or used to construct credentials.
let account = null;
export function offlineMusicMode() {
 return new URLSearchParams(globalThis.location?.search || '').get('offline') === 'music';
}
export function localFilesMode() {
 return new URLSearchParams(globalThis.location?.search || '').get('local') === 'files';
}
export function localPodcastsMode() {
 return new URLSearchParams(globalThis.location?.search || '').get('local') === 'podcasts';
}
export function deviceLibraryMode() {
 return offlineMusicMode() || localFilesMode() || localPodcastsMode();
}
export function offlineMusicAccount() { return offlineMusicMode() ? account : null; }
export function selectOfflineMusicAccount(id) {
 if (!offlineMusicMode()) throw Error('Open Offline music first.');
 account = Number.isSafeInteger(id) && id > 0 ? id : null;
}
