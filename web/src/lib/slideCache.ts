import { createBlobCache, type CacheStats, type CachedBlobMetadata } from './blobCache';

const slideCache = createBlobCache('slidetalk-slide-cache', 'slides');

export type CachedSlide = CachedBlobMetadata;
export type SlideCacheStats = CacheStats;

export function slideCacheAvailable() {
  return slideCache.available();
}

export function getCachedSlide(sha256: string): Promise<CachedSlide | null> {
  return slideCache.get(sha256);
}

export function putCachedSlide(input: Omit<CachedSlide, 'lastAccessedAt' | 'createdAt'>): Promise<void> {
  return slideCache.put(input);
}

export function gcSlideCache(now = Date.now()): Promise<void> {
  return slideCache.gc(now);
}

export function slideCacheStats(): Promise<SlideCacheStats> {
  return slideCache.stats();
}

export function clearSlideCache(): Promise<void> {
  return slideCache.clear();
}
