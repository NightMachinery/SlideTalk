import { cacheLimits } from './cacheConstants';
export { cacheLimits } from './cacheConstants';

export type CachedBlobMetadata = {
  sha256: string;
  blob: Blob;
  mimeType: string;
  originalName: string;
  sizeBytes: number;
  lastAccessedAt: number;
  createdAt: number;
};

export type CacheStats = {
  entries: number;
  bytes: number;
};

export type BlobCache = {
  available(): boolean;
  get(sha256: string): Promise<CachedBlobMetadata | null>;
  list(): Promise<CachedBlobMetadata[]>;
  put(input: Omit<CachedBlobMetadata, 'lastAccessedAt' | 'createdAt'>): Promise<void>;
  gc(now?: number): Promise<void>;
  stats(): Promise<CacheStats>;
  clear(): Promise<void>;
};

export function cacheStats(entries: Pick<CachedBlobMetadata, 'sizeBytes'>[]): CacheStats {
  return {
    entries: entries.length,
    bytes: entries.reduce((sum, entry) => sum + entry.sizeBytes, 0)
  };
}

export function listCacheEntries<T extends CachedBlobMetadata>(entries: T[]): T[] {
  return [...entries];
}

export function selectCacheRemovals(
  entries: Pick<CachedBlobMetadata, 'sha256' | 'sizeBytes' | 'lastAccessedAt'>[],
  limits = cacheLimits,
  now = Date.now()
): string[] {
  const sorted = [...entries].sort((a, b) => a.lastAccessedAt - b.lastAccessedAt);
  let total = sorted.reduce((sum, entry) => sum + entry.sizeBytes, 0);
  let count = sorted.length;
  const remove: string[] = [];
  for (const entry of sorted) {
    if (now - entry.lastAccessedAt <= limits.maxAgeMs && total <= limits.maxBytes && count <= limits.maxEntries) continue;
    remove.push(entry.sha256);
    total -= entry.sizeBytes;
    count -= 1;
  }
  return remove;
}

export function createBlobCache(dbName: string, storeName: string, limits = cacheLimits): BlobCache {
  async function openDB(): Promise<IDBDatabase | null> {
    if (typeof indexedDB === 'undefined') return null;
    return new Promise((resolve, reject) => {
      const open = indexedDB.open(dbName, 1);
      open.onupgradeneeded = () => {
        const db = open.result;
        if (!db.objectStoreNames.contains(storeName)) {
          db.createObjectStore(storeName, { keyPath: 'sha256' });
        }
      };
      open.onsuccess = () => resolve(open.result);
      open.onerror = () => reject(open.error ?? new Error(`Could not open ${dbName}.`));
    });
  }

  async function entries(db: IDBDatabase): Promise<CachedBlobMetadata[]> {
    return request<CachedBlobMetadata[]>(db.transaction(storeName, 'readonly').objectStore(storeName).getAll());
  }

  return {
    available() {
      return typeof indexedDB !== 'undefined';
    },
    async get(sha256: string) {
      const db = await openDB();
      if (!db) return null;
      const cached = await request<CachedBlobMetadata | undefined>(db.transaction(storeName, 'readonly').objectStore(storeName).get(sha256));
      if (!cached) {
        db.close();
        return null;
      }
      cached.lastAccessedAt = Date.now();
      await request(db.transaction(storeName, 'readwrite').objectStore(storeName).put(cached));
      db.close();
      return cached;
    },
    async list() {
      const db = await openDB();
      if (!db) return [];
      const cached = listCacheEntries(await entries(db));
      db.close();
      return cached;
    },
    async put(input) {
      const db = await openDB();
      if (!db) return;
      const now = Date.now();
      await request(db.transaction(storeName, 'readwrite').objectStore(storeName).put({ ...input, lastAccessedAt: now, createdAt: now }));
      db.close();
      await this.gc();
    },
    async gc(now = Date.now()) {
      const db = await openDB();
      if (!db) return;
      const remove = selectCacheRemovals(await entries(db), limits, now);
      if (remove.length > 0) {
        const store = db.transaction(storeName, 'readwrite').objectStore(storeName);
        await Promise.all(remove.map((sha256) => request(store.delete(sha256))));
      }
      db.close();
    },
    async stats() {
      const db = await openDB();
      if (!db) return { entries: 0, bytes: 0 };
      const stats = cacheStats(await entries(db));
      db.close();
      return stats;
    },
    async clear() {
      const db = await openDB();
      if (!db) return;
      await request(db.transaction(storeName, 'readwrite').objectStore(storeName).clear());
      db.close();
    }
  };
}

function request<T = unknown>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error('Cache request failed.'));
  });
}
