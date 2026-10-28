import assert from 'node:assert/strict';
import { localFileId, localFileToMedia } from '../local-files-store.js';
import { localFilesMode, deviceLibraryMode } from '../offline-context.js';

const file = { name: 'song.mp3', size: 12, lastModified: 99, type: 'audio/mpeg' };
assert.equal(localFileId(file), 'local:song.mp3:12:99');

const media = localFileToMedia({ id: 'local:1', name: 'song.mp3', type: 'audio', blob: new Blob(['x']) });
assert.match(media.offlinePinURL, /^blob:/);
assert.equal(media.type, 'audio');
assert.equal(media.localFile, true);
URL.revokeObjectURL(media.offlinePinURL);

const prev = globalThis.location;
globalThis.location = { search: '?local=files' };
assert.equal(localFilesMode(), true);
assert.equal(deviceLibraryMode(), true);
globalThis.location = { search: '?offline=music' };
assert.equal(deviceLibraryMode(), false, "legacy offline music URLs use the online app");
assert.equal(localFilesMode(), false);
globalThis.location = prev || { search: '' };
console.log('local-files unit checks passed');
