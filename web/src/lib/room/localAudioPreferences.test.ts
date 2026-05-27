import { afterEach, describe, expect, it, vi } from 'vitest';
import { loadLocalAudioPreferences, saveLocalAudioPreferences } from './localAudioPreferences';

describe('local audio preferences', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
  });

  it('stores mute and volume separately for each room and user', () => {
    saveLocalAudioPreferences('room-one', 'user-one', { muted: true, volume: 0.4 });
    saveLocalAudioPreferences('room-one', 'user-two', { muted: false, volume: 0.8 });
    saveLocalAudioPreferences('room-two', 'user-one', { muted: false, volume: 0.2 });

    expect(loadLocalAudioPreferences('room-one', 'user-one')).toEqual({ muted: true, volume: 0.4 });
    expect(loadLocalAudioPreferences('room-one', 'user-two')).toEqual({ muted: false, volume: 0.8 });
    expect(loadLocalAudioPreferences('room-two', 'user-one')).toEqual({ muted: false, volume: 0.2 });
  });

  it('defaults to unmuted full volume when no preference exists', () => {
    expect(loadLocalAudioPreferences('room-one', 'user-one')).toEqual({ muted: false, volume: 1 });
  });

  it('clamps invalid and out-of-range volume values', () => {
    localStorage.setItem('slidetalk.roomAudioPrefs.v1:room-one:user-one', JSON.stringify({ muted: true, volume: 4 }));
    localStorage.setItem('slidetalk.roomAudioPrefs.v1:room-one:user-two', JSON.stringify({ muted: false, volume: -1 }));
    localStorage.setItem('slidetalk.roomAudioPrefs.v1:room-one:user-three', JSON.stringify({ muted: false, volume: Number.NaN }));

    expect(loadLocalAudioPreferences('room-one', 'user-one')).toEqual({ muted: true, volume: 1 });
    expect(loadLocalAudioPreferences('room-one', 'user-two')).toEqual({ muted: false, volume: 0 });
    expect(loadLocalAudioPreferences('room-one', 'user-three')).toEqual({ muted: false, volume: 1 });
  });

  it('falls back to the legacy per-room mute key when no per-user preference exists', () => {
    localStorage.setItem('slidetalk.roomAudioMute.v1:room-one', 'true');

    expect(loadLocalAudioPreferences('room-one', 'user-one')).toEqual({ muted: true, volume: 1 });
  });

  it('ignores invalid data and blocked storage', () => {
    localStorage.setItem('slidetalk.roomAudioPrefs.v1:room-one:user-one', 'sometimes');
    expect(loadLocalAudioPreferences('room-one', 'user-one')).toEqual({ muted: false, volume: 1 });

    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    expect(() => saveLocalAudioPreferences('room-one', 'user-one', { muted: true, volume: 0.5 })).not.toThrow();
  });
});
