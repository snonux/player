// Explicit local-download mode is separate from authenticated server identity.
// It is never persisted as an online user or used to construct credentials.
let account = null;
export function offlineMusicMode() {
 return new URLSearchParams(globalThis.location?.search || '').get('offline') === 'music';
}
export function offlineMusicAccount() { return offlineMusicMode() ? account : null; }
export function selectOfflineMusicAccount(id) {
 if (!offlineMusicMode()) throw Error('Open Offline music first.');
 account = Number.isSafeInteger(id) && id > 0 ? id : null;
}
