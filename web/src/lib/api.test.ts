import { afterEach, describe, expect, it, vi } from 'vitest';
import { getRoomSnapshot } from './api';

describe('getRoomSnapshot', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
  });

  it('fetches the room snapshot endpoint', async () => {
    localStorage.setItem('slidetalk.authToken', 'token-one');
    const fetchMock = vi.fn(async () => {
      return new Response(
        JSON.stringify({
          room: { id: 'room-one', title: 'Live', hasPassword: false, noSlideMode: false, allowParticipantMarkdown: false, raiseHandMode: 'off', slidePage: 1, sharedNavigationEnabled: true },
          caller: { userId: 'user-one', role: 'mod', isAdmin: true },
          participants: [],
          observers: [],
          currentTurn: { currentSpeakerUserId: '', nextSpeakerUserId: '' },
          timer: { state: 'stopped', durationSeconds: 0, startedAt: null, serverNow: '2026-05-21T00:00:00Z' },
          hands: [],
          slide: null,
          markdown: '',
          markdownUpdatedByUserId: '',
          markdownUpdatedByName: '',
          markdownUpdatedAt: ''
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      );
    });
    vi.stubGlobal('fetch', fetchMock);

    const snapshot = await getRoomSnapshot('room-one');

    expect(snapshot.room.id).toBe('room-one');
    expect(fetchMock).toHaveBeenCalledWith('/api/rooms/room-one/snapshot', expect.objectContaining({
      headers: expect.objectContaining({ Authorization: 'Bearer token-one' })
    }));
  });
});
