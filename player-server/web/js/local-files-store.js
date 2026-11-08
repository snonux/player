// Best-effort IndexedDB store for browser-picked local media blobs.
import { openIndexedDB, txDone, allRows } from './idb.js';

const DB = 'player-local-files-v1';
const STORE = 'files';

function openDB() {
  return openIndexedDB(DB, 1, (db) => {
    if (!db.objectStoreNames.contains(STORE)) {
      db.createObjectStore(STORE, { keyPath: 'id' });
    }
  });
}


export function localFileId(file) {
  return `local:${file.name}:${file.size}:${file.lastModified}`;
}

export async function listLocalFiles() {
  const db = await openDB();
  try {
    const tx = db.transaction(STORE, 'readonly');
    const req = tx.objectStore(STORE).getAll();
    const rows = await allRows(req);
    await txDone(tx);
    return rows.sort((a, b) => String(a.name).localeCompare(String(b.name)));
  } finally {
    db.close();
  }
}

export async function addLocalFiles(fileList) {
  const files = [...fileList].filter(file => /^(audio|video)\//.test(file.type) || /\.(mp3|m4a|flac|ogg|opus|wav|aac|mp4|webm|mkv|mov)$/i.test(file.name));
  if (!files.length) return [];
  const db = await openDB();
  try {
    const tx = db.transaction(STORE, 'readwrite');
    const store = tx.objectStore(STORE);
    const saved = [];
    for (const file of files) {
      const row = {
        id: localFileId(file),
        name: file.name,
        type: file.type.startsWith('video/') ? 'video' : 'audio',
        mime: file.type || 'application/octet-stream',
        size: file.size,
        lastModified: file.lastModified,
        blob: file,
      };
      store.put(row);
      saved.push(row);
    }
    await txDone(tx);
    return saved;
  } finally {
    db.close();
  }
}

export async function removeLocalFile(id) {
  const db = await openDB();
  try {
    const tx = db.transaction(STORE, 'readwrite');
    tx.objectStore(STORE).delete(id);
    await txDone(tx);
  } finally {
    db.close();
  }
}

export function localFileToMedia(row) {
  const url = URL.createObjectURL(row.blob);
  return {
    id: row.id,
    type: row.type,
    file_name: row.name,
    duration: 0,
    offlinePinURL: url,
    localFile: true,
  };
}
