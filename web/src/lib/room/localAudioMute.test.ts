import { afterEach, describe, expect, it } from 'vitest';
import { loadLocalAudioMute, saveLocalAudioMute } from './localAudioMute';

describe('local audio mute preferences', () => {
  afterEach(() => {
    localStorage.clear();
  });

  it('stores mute state separately for each room', () => {
    saveLocalAudioMute('room-one', true);
    saveLocalAudioMute('room-two', false);

    expect(loadLocalAudioMute('room-one')).toBe(true);
    expect(loadLocalAudioMute('room-two')).toBe(false);
  });

  it('defaults to unmuted when a room has no saved preference or invalid data', () => {
    localStorage.setItem('slidetalk.roomAudioMute.v1:room-one', 'sometimes');

    expect(loadLocalAudioMute('room-one')).toBe(false);
    expect(loadLocalAudioMute('room-two')).toBe(false);
  });
});
