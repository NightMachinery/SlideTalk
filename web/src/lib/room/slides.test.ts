import { describe, expect, it } from 'vitest';
import { pageFromSharedNavigation } from './slides';

describe('pageFromSharedNavigation', () => {
  it('keeps local page when follow is disabled', () => {
    expect(pageFromSharedNavigation({ localPage: 5, sharedPage: 2, followShared: false })).toBe(5);
  });

  it('applies shared page when follow is enabled', () => {
    expect(pageFromSharedNavigation({ localPage: 5, sharedPage: 2, followShared: true })).toBe(2);
  });
});
