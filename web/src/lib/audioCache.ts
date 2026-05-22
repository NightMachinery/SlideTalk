import type { RoomSnapshot } from './realtime';

const dbName = 'slidetalk-audio-cache';
const storeName = 'audio';
const dbVersion = 1;
const maxAgeMs = 30 * 24 * 60 * 60 * 1000;
const maxBytes = 1024 * 1024 * 1024;
const maxEntries = 30;

export type CachedAudio = {
  sha256: string;
  blob: Blob;
  mimeType: string;
  originalName: string;
  sizeBytes: number;
  lastAccessedAt: number;
  createdAt: number;
};

type StoredAudio = CachedAudio;

export function audioCacheAvailable() {
  return typeof indexedDB !== 'undefined';
}

export async function getCachedAudio(sha256: string): Promise<CachedAudio | null> {
  const db = await openDB();
  if (!db) return null;
  const cached = await request<StoredAudio | undefined>(db.transaction(storeName, 'readonly').objectStore(storeName).get(sha256));
  if (!cached) {
    db.close();
    return null;
  }
  cached.lastAccessedAt = Date.now();
  await request(db.transaction(storeName, 'readwrite').objectStore(storeName).put(cached));
  db.close();
  return cached;
}

export async function putCachedAudio(input: Omit<CachedAudio, 'lastAccessedAt' | 'createdAt'>): Promise<void> {
  const db = await openDB();
  if (!db) return;
  const now = Date.now();
  await request(db.transaction(storeName, 'readwrite').objectStore(storeName).put({ ...input, lastAccessedAt: now, createdAt: now }));
  db.close();
  await gcAudioCache();
}

export async function gcAudioCache(now = Date.now()): Promise<void> {
  const db = await openDB();
  if (!db) return;
  const store = db.transaction(storeName, 'readonly').objectStore(storeName);
  const entries = (await request<StoredAudio[]>(store.getAll())).sort((a, b) => a.lastAccessedAt - b.lastAccessedAt);
  let total = entries.reduce((sum, entry) => sum + entry.sizeBytes, 0);
  let count = entries.length;
  const remove: string[] = [];
  for (const entry of entries) {
    if (now - entry.lastAccessedAt <= maxAgeMs && total <= maxBytes && count <= maxEntries) continue;
    remove.push(entry.sha256);
    total -= entry.sizeBytes;
    count -= 1;
  }
  if (remove.length > 0) {
    const writeStore = db.transaction(storeName, 'readwrite').objectStore(storeName);
    await Promise.all(remove.map((sha256) => request(writeStore.delete(sha256))));
  }
  db.close();
}

export function trackDisplayTitle(track: RoomSnapshot['audio']['tracks'][number] | undefined) {
  return track?.title || track?.metadataTitle || fileNameWithoutExtension(track?.originalName ?? '') || 'No audio selected';
}

export function trackUploaderName(track: RoomSnapshot['audio']['tracks'][number] | undefined) {
  return track?.uploaderDisplayName || track?.uploadedByName || track?.uploadedByUserId || '';
}

export function fileNameWithoutExtension(name: string) {
  const clean = name.trim();
  const dot = clean.lastIndexOf('.');
  if (dot <= 0) return clean;
  return clean.slice(0, dot);
}

export function audioSubtype(mimeType: string) {
  return mimeType.toLowerCase().replace(/^audio\//, '');
}

function openDB(): Promise<IDBDatabase | null> {
  if (!audioCacheAvailable()) return Promise.resolve(null);
  return new Promise((resolve, reject) => {
    const open = indexedDB.open(dbName, dbVersion);
    open.onupgradeneeded = () => {
      const db = open.result;
      if (!db.objectStoreNames.contains(storeName)) {
        db.createObjectStore(storeName, { keyPath: 'sha256' });
      }
    };
    open.onsuccess = () => resolve(open.result);
    open.onerror = () => reject(open.error ?? new Error('Could not open audio cache.'));
  });
}

function request<T = unknown>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error('Audio cache request failed.'));
  });
}
