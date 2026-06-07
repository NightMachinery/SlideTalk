import { describe, expect, it } from 'vitest';
import { nextLocalAudioTrackId } from './localAudioMode';

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
});
