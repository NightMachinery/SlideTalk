import { describe, expect, it } from 'vitest';
import { nextLocalAudioTrackId, previousLocalAudioTrackId } from './localAudioMode';

const tracks = [{ id: 'first' }, { id: 'second' }, { id: 'third' }];

describe('nextLocalAudioTrackId', () => {
  it('stops for stop mode and next/previous edges', () => {
    expect(nextLocalAudioTrackId('room-one', tracks, 'first', 'stop')).toBe('');
    expect(nextLocalAudioTrackId('room-one', tracks, 'third', 'next')).toBe('');
    expect(nextLocalAudioTrackId('room-one', tracks, 'first', 'previous')).toBe('');
  });

  it('advances and wraps for repeat modes', () => {
    expect(nextLocalAudioTrackId('room-one', tracks, 'second', 'next')).toBe('third');
    expect(nextLocalAudioTrackId('room-one', tracks, 'second', 'previous')).toBe('first');
    expect(nextLocalAudioTrackId('room-one', tracks, 'third', 'repeat-forward')).toBe('first');
    expect(nextLocalAudioTrackId('room-one', tracks, 'first', 'repeat-backward')).toBe('third');
    expect(nextLocalAudioTrackId('room-one', tracks, 'second', 'repeat-one')).toBe('second');
  });

  it('keeps shuffle stable for the same room and track list', () => {
    const first = nextLocalAudioTrackId('room-one', tracks, 'first', 'shuffle');
    const second = nextLocalAudioTrackId('room-one', tracks, 'first', 'shuffle');

    expect(first).toBeTruthy();
    expect(first).not.toBe('first');
    expect(second).toBe(first);
  });

  it('makes shuffle next and previous inverse operations', () => {
    for (const track of tracks) {
      const next = nextLocalAudioTrackId('room-one', tracks, track.id, 'shuffle');
      const previous = previousLocalAudioTrackId('room-one', tracks, track.id, 'shuffle');

      expect(previousLocalAudioTrackId('room-one', tracks, next, 'shuffle')).toBe(track.id);
      expect(nextLocalAudioTrackId('room-one', tracks, previous, 'shuffle')).toBe(track.id);
    }
  });

  it('returns the only track for single-track shuffle in both directions', () => {
    const singleTrack = [{ id: 'only' }];

    expect(nextLocalAudioTrackId('room-one', singleTrack, 'only', 'shuffle')).toBe('only');
    expect(previousLocalAudioTrackId('room-one', singleTrack, 'only', 'shuffle')).toBe('only');
  });

});
