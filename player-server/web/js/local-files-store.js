// Best-effort IndexedDB store for browser-picked local media blobs.
const DB = 'player-local-files-v1';
const STORE = 'files';

function openDB() {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB, 1);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(STORE)) {
        db.createObjectStore(STORE, { keyPath: 'id' });
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error || new Error('IndexedDB unavailable'));
  });
}

function txDone(tx) {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error || new Error('IndexedDB transaction failed'));
    tx.onabort = () => reject(tx.error || new Error('IndexedDB transaction aborted'));
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
    const rows = await new Promise((resolve, reject) => {
      req.onsuccess = () => resolve(req.result || []);
      req.onerror = () => reject(req.error);
    });
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
