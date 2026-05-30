import { describe, expect, it } from 'vitest';
import { cacheStats, listCacheEntries, selectCacheRemovals, type CachedBlobMetadata } from './blobCache';
import { cacheLimits } from './cacheConstants';

function entry(input: Partial<CachedBlobMetadata> & Pick<CachedBlobMetadata, 'sha256'>): CachedBlobMetadata {
  return {
    blob: new Blob(['x']),
    mimeType: 'application/octet-stream',
    originalName: `${input.sha256}.bin`,
    sizeBytes: 100,
    lastAccessedAt: 1000,
    createdAt: 1000,
    ...input
  };
}

describe('blob cache policy', () => {
  it('uses 2 GiB and 500 entries as the shared cache limits', () => {
    expect(cacheLimits.maxBytes).toBe(2 * 1024 * 1024 * 1024);
    expect(cacheLimits.maxEntries).toBe(500);
  });

  it('summarizes cache usage', () => {
    const stats = cacheStats([entry({ sha256: 'a', sizeBytes: 120 }), entry({ sha256: 'b', sizeBytes: 80 })]);

    expect(stats).toEqual({ entries: 2, bytes: 200 });
  });

  it('returns cache entries in read order without touching access times', () => {
    const entries = [entry({ sha256: 'a', lastAccessedAt: 1 }), entry({ sha256: 'b', lastAccessedAt: 2 })];

    expect(listCacheEntries(entries)).toEqual(entries);
  });

  it('evicts least recently used entries to satisfy byte and entry limits', () => {
    const entries = [
      entry({ sha256: 'oldest', sizeBytes: 100, lastAccessedAt: 1 }),
      entry({ sha256: 'middle', sizeBytes: 100, lastAccessedAt: 2 }),
      entry({ sha256: 'newest', sizeBytes: 100, lastAccessedAt: 3 })
    ];

    expect(selectCacheRemovals(entries, { maxAgeMs: 1000, maxBytes: 200, maxEntries: 2 }, 10)).toEqual(['oldest']);
  });
});
