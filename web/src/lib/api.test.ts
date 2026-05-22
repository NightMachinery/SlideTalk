import { afterEach, describe, expect, it, vi } from 'vitest';
import { createAudioDownloadLink, createRoom, getRoomSnapshot, updateRoomAudio } from './api';

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
          room: { id: 'room-one', title: 'Live', hasPassword: false, roomMode: 'slides', allowParticipantMarkdown: false, raiseHandMode: 'off', slidePage: 1, sharedNavigationEnabled: true },
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

describe('createRoom', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
  });

  it('sends room mode when creating a room', async () => {
    localStorage.setItem('slidetalk.authToken', 'token-one');
    const fetchMock = vi.fn(async () => {
      return new Response(
        JSON.stringify({
          room: { id: 'room-one', title: 'Listening', hasPassword: false },
          membership: { roomId: 'room-one', userId: 'user-one', role: 'mod', displayOrder: 0 }
        }),
        { status: 201, headers: { 'Content-Type': 'application/json' } }
      );
    });
    vi.stubGlobal('fetch', fetchMock);

    await createRoom('Listening', '', 'audio');

    expect(fetchMock).toHaveBeenCalledWith('/api/rooms', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ title: 'Listening', password: '', roomMode: 'audio' })
    }));
  });
});

describe('audio API helpers', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
  });

  it('creates tokenized audio download links', async () => {
    localStorage.setItem('slidetalk.authToken', 'token-one');
    const fetchMock = vi.fn(async () => {
      return new Response(JSON.stringify({ url: '/api/rooms/room-one/audio/track-one?downloadToken=secret' }), { status: 201, headers: { 'Content-Type': 'application/json' } });
    });
    vi.stubGlobal('fetch', fetchMock);

    const link = await createAudioDownloadLink('room-one', 'track-one');

    expect(link.url).toContain('downloadToken=');
    expect(fetchMock).toHaveBeenCalledWith('/api/rooms/room-one/audio/track-one/download-link', expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({ Authorization: 'Bearer token-one' })
    }));
  });

  it('patches audio display metadata', async () => {
    localStorage.setItem('slidetalk.authToken', 'token-one');
    const fetchMock = vi.fn(async () => new Response(null, { status: 204 }));
    vi.stubGlobal('fetch', fetchMock);

    await updateRoomAudio('room-one', 'track-one', { title: 'New title', uploaderDisplayName: 'Guest' });

    expect(fetchMock).toHaveBeenCalledWith('/api/rooms/room-one/audio/track-one', expect.objectContaining({
      method: 'PATCH',
      body: JSON.stringify({ title: 'New title', uploaderDisplayName: 'Guest' })
    }));
  });
});
