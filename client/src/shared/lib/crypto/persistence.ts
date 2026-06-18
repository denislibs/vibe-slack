// Per-device persistence of the OpenMLS engine state in IndexedDB. The crypto
// worker exports its state (identity + groups) as bytes; we store one blob per
// device name so a page reload can rebuild the engine instead of losing all
// group keys (which caused "unknown group" on send after refresh).
const DB_NAME = "messenger-crypto";
const STORE = "engine-state";

function openDb(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, 1);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(STORE)) db.createObjectStore(STORE);
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

export async function saveCryptoState(name: string, state: Uint8Array): Promise<void> {
  const db = await openDb();
  try {
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction(STORE, "readwrite");
      tx.objectStore(STORE).put(state.slice(), name);
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
    });
  } finally {
    db.close();
  }
}

export async function loadCryptoState(name: string): Promise<Uint8Array | null> {
  const db = await openDb();
  try {
    return await new Promise<Uint8Array | null>((resolve, reject) => {
      const tx = db.transaction(STORE, "readonly");
      const req = tx.objectStore(STORE).get(name);
      req.onsuccess = () => {
        const v = req.result as ArrayBuffer | Uint8Array | undefined;
        if (v == null) resolve(null);
        else resolve(v instanceof Uint8Array ? v : new Uint8Array(v));
      };
      req.onerror = () => reject(req.error);
    });
  } finally {
    db.close();
  }
}
