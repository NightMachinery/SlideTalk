const localAudioMutePrefix = 'slidetalk.roomAudioMute.v1:';

function keyForRoom(roomId: string) {
  return `${localAudioMutePrefix}${roomId}`;
}

export function loadLocalAudioMute(roomId: string) {
  if (!roomId) return false;
  try {
    return localStorage.getItem(keyForRoom(roomId)) === 'true';
  } catch {
    return false;
  }
}

export function saveLocalAudioMute(roomId: string, muted: boolean) {
  if (!roomId) return;
  try {
    localStorage.setItem(keyForRoom(roomId), muted ? 'true' : 'false');
  } catch {
    // Local mute is a client preference; blocked storage should not break audio controls.
  }
}
