// On-device modes are separate from authenticated server identity.
export function localFilesMode() {
 return new URLSearchParams(globalThis.location?.search || '').get('local') === 'files';
}
export function deviceLibraryMode() {
 return localFilesMode();
}
