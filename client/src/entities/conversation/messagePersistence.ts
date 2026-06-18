// Per-device persistence of conversation messages in IndexedDB. The MLS engine
// cannot decrypt the user's OWN sent messages from server history, so the only
// durable copy of what the user sent is local — we persist the decrypted/echoed
// message store and rehydrate it on reload.
import type { ChatMessage } from "./store";

const DB_NAME = "messenger-conversations";
const STORE = "messages";

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

export async function loadMessages(deviceName: string): Promise<Record<string, ChatMessage[]> | null> {
  const db = await openDb();
  try {
    return await new Promise((resolve, reject) => {
      const tx = db.transaction(STORE, "readonly");
      const req = tx.objectStore(STORE).get(deviceName);
      req.onsuccess = () => {
        const v = req.result as string | undefined;
        if (v == null) return resolve(null);
        try { resolve(JSON.parse(v) as Record<string, ChatMessage[]>); }
        catch { resolve(null); }
      };
      req.onerror = () => reject(req.error);
    });
  } finally { db.close(); }
}

export async function saveMessages(deviceName: string, byGroup: Record<string, ChatMessage[]>): Promise<void> {
  const db = await openDb();
  try {
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction(STORE, "readwrite");
      tx.objectStore(STORE).put(JSON.stringify(byGroup), deviceName);
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
    });
  } finally { db.close(); }
}
