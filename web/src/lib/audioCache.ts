import { createBlobCache, type CacheStats, type CachedBlobMetadata } from './blobCache';
import type { RoomSnapshot } from './realtime';

const audioCache = createBlobCache('slidetalk-audio-cache', 'audio');
export const hiddenUploaderDisplayName = '__slidetalk_hidden_uploader__';

export type CachedAudio = CachedBlobMetadata;
export type AudioCacheStats = CacheStats;

export function audioCacheAvailable() {
  return audioCache.available();
}

export function getCachedAudio(sha256: string): Promise<CachedAudio | null> {
  return audioCache.get(sha256);
}

export function listCachedAudio(): Promise<CachedAudio[]> {
  return audioCache.list();
}

export function putCachedAudio(input: Omit<CachedAudio, 'lastAccessedAt' | 'createdAt'>): Promise<void> {
  return audioCache.put(input);
}

export function gcAudioCache(now = Date.now()): Promise<void> {
  return audioCache.gc(now);
}

export function audioCacheStats(): Promise<AudioCacheStats> {
  return audioCache.stats();
}

export function clearAudioCache(): Promise<void> {
  return audioCache.clear();
}

export function trackDisplayTitle(track: RoomSnapshot['audio']['tracks'][number] | undefined) {
  return track?.title || track?.metadataTitle || fileNameWithoutExtension(track?.originalName ?? '') || 'No audio selected';
}

export function trackUploaderName(track: RoomSnapshot['audio']['tracks'][number] | undefined) {
  if (track?.uploaderDisplayName === hiddenUploaderDisplayName) return '';
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
