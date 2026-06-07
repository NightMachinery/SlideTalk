import type { RoomSnapshot } from '../realtime';

export type AudioTrackRef = Pick<RoomSnapshot['audio']['tracks'][number], 'id'>;
export type AudioPlaybackMode = RoomSnapshot['audio']['playbackMode'];

export function nextLocalAudioTrackId(roomId: string, tracks: AudioTrackRef[], currentId: string, mode: AudioPlaybackMode): string {
  if (tracks.length === 0 || mode === 'stop') return '';
  if (mode === 'repeat-one') return currentId;
  if (mode === 'shuffle') return nextShuffledLocalAudioTrackId(roomId, tracks, currentId);
  const index = tracks.findIndex((track) => track.id === currentId);
  if (index < 0) return tracks[0]?.id ?? '';
  if (mode === 'previous' || mode === 'repeat-backward') {
    if (index === 0) return mode === 'repeat-backward' ? tracks[tracks.length - 1].id : '';
    return tracks[index - 1].id;
  }
  if (index + 1 >= tracks.length) return mode === 'repeat-forward' ? tracks[0].id : '';
  return tracks[index + 1].id;
}

function nextShuffledLocalAudioTrackId(roomId: string, tracks: AudioTrackRef[], currentId: string): string {
  if (tracks.length === 0) return '';
  if (tracks.length === 1) return tracks[0].id;
  const shuffled = [...tracks].sort((a, b) => {
    const aKey = deterministicShuffleKey(roomId, a.id);
    const bKey = deterministicShuffleKey(roomId, b.id);
    if (aKey < bKey) return -1;
    if (aKey > bKey) return 1;
    return a.id.localeCompare(b.id);
  });
  const index = shuffled.findIndex((track) => track.id === currentId);
  if (index < 0 || index + 1 >= shuffled.length) return shuffled[0].id;
  return shuffled[index + 1].id;
}

function deterministicShuffleKey(roomId: string, trackId: string): bigint {
  const input = `${roomId}:${trackId}`;
  let hash = 0xcbf29ce484222325n;
  const prime = 0x100000001b3n;
  const mask = 0xffffffffffffffffn;
  for (let index = 0; index < input.length; index += 1) {
    hash ^= BigInt(input.charCodeAt(index));
    hash = (hash * prime) & mask;
  }
  return hash;
}
