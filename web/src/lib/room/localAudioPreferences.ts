const localAudioPreferencesPrefix = 'slidetalk.roomAudioPrefs.v1:';
const legacyLocalAudioMutePrefix = 'slidetalk.roomAudioMute.v1:';

export type LocalAudioPreferences = {
  muted: boolean;
  volume: number;
};

const defaultLocalAudioPreferences: LocalAudioPreferences = {
  muted: false,
  volume: 1
};

function keyForRoomUser(roomId: string, userId: string) {
  return `${localAudioPreferencesPrefix}${roomId}:${userId}`;
}

function legacyMuteKeyForRoom(roomId: string) {
  return `${legacyLocalAudioMutePrefix}${roomId}`;
}

function clampVolume(volume: unknown) {
  if (typeof volume !== 'number' || !Number.isFinite(volume)) return defaultLocalAudioPreferences.volume;
  return Math.min(1, Math.max(0, volume));
}

export function loadLocalAudioPreferences(roomId: string, userId: string): LocalAudioPreferences {
  if (!roomId || !userId) return { ...defaultLocalAudioPreferences };
  try {
    const raw = localStorage.getItem(keyForRoomUser(roomId, userId));
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<LocalAudioPreferences>;
      return {
        muted: parsed.muted === true,
        volume: clampVolume(parsed.volume)
      };
    }
    return {
      muted: localStorage.getItem(legacyMuteKeyForRoom(roomId)) === 'true',
      volume: defaultLocalAudioPreferences.volume
    };
  } catch {
    return { ...defaultLocalAudioPreferences };
  }
}

export function saveLocalAudioPreferences(roomId: string, userId: string, preferences: LocalAudioPreferences) {
  if (!roomId || !userId) return;
  try {
    localStorage.setItem(
      keyForRoomUser(roomId, userId),
      JSON.stringify({
        muted: preferences.muted === true,
        volume: clampVolume(preferences.volume)
      })
    );
  } catch {
    // Local audio preferences should not break playback controls when storage is blocked.
  }
}
