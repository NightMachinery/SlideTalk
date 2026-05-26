import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import App from './App.svelte';

const roomDetails = {
  room: { id: 'room-one', title: 'Planning Circle', hasPassword: false },
  membership: { roomId: 'room-one', userId: 'user-one', role: 'participant', displayOrder: 1 }
};

const roomSnapshot = {
  room: {
    id: 'room-one',
    title: 'Planning Circle',
    hasPassword: false,
    roomMode: 'slides',
    allowParticipantMarkdown: false,
    raiseHandMode: 'off',
    slidePage: 1,
    sharedNavigationEnabled: true
  },
  caller: { userId: 'user-one', role: 'participant', isAdmin: false },
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
};

class TestWebSocket {
  static sockets: TestWebSocket[] = [];
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 3;
  readyState = TestWebSocket.OPEN;
  onopen: ((event: Event) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  sent: string[] = [];

  constructor(readonly url: string) {
    TestWebSocket.sockets.push(this);
  }

  send(message: string) {
    this.sent.push(message);
  }

  close() {
    this.readyState = TestWebSocket.CLOSED;
    this.onclose?.(new CloseEvent('close'));
  }

  receive(value: unknown) {
    this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(value) }));
  }
}

function response(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

function mockFetch(user: { id: string; displayName: string; isAdmin: boolean }, snapshot = roomSnapshot) {
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = input.toString();
    const method = init?.method ?? 'GET';

    if (url === '/api/me' && method === 'GET') return response(user);
    if (url === '/api/me' && method === 'PATCH') {
      const body = JSON.parse(init?.body as string) as { displayName: string };
      user.displayName = body.displayName.trim();
      return response(user);
    }
    if (url === '/api/admins') {
      return response([{ id: user.id, displayName: user.displayName, createdAt: '2026-05-21T00:00:00Z' }]);
    }
    if (url === '/api/rooms/room-one') return response(roomDetails);
    if (url === '/api/rooms/room-one/snapshot') return response(snapshot);
    if (url === '/api/rooms/room-one/ws-ticket') return response({ ticket: 'ticket-one', expiresAt: '2026-05-21T00:01:00Z' });

    throw new Error(`Unhandled request: ${method} ${url}`);
  });
}

describe('App landing polish', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    localStorage.clear();
    TestWebSocket.sockets = [];
    window.history.replaceState({}, '', '/');
    document.title = 'SlideTalk';
  });

  it('hides the bootstrap admin token form for admins', async () => {
    vi.stubGlobal('fetch', mockFetch({ id: 'admin-one', displayName: 'Ada', isAdmin: true }));

    render(App);

    await screen.findByText('Site admin');
    expect(screen.queryByText('Admin token')).toBeNull();
    expect(screen.queryByText('Bootstrap token')).toBeNull();
  });

  it('keeps admin tools collapsed by default for non-admins', async () => {
    vi.stubGlobal('fetch', mockFetch({ id: 'user-one', displayName: 'Grace', isAdmin: false }));

    render(App);

    await screen.findByRole('heading', { name: 'Open a roundtable room.' });
    const adminDisclosure = document.querySelector<HTMLDetailsElement>('details.admin-disclosure');
    expect(adminDisclosure?.open).toBe(false);
    expect(screen.getByText('Bootstrap token')).toBeTruthy();
  });

  it('shows a name gate for room links when the profile has no saved name', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    vi.stubGlobal('fetch', mockFetch({ id: 'user-one', displayName: '', isAdmin: false }));

    render(App);

    await screen.findByRole('heading', { name: 'Choose a display name' });
    expect(screen.queryByRole('heading', { name: 'Create room' })).toBeNull();
    expect(screen.queryByRole('heading', { name: 'Join room' })).toBeNull();
  });

  it('saves the name gate and opens the linked room without an opened-room notice', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    const fetchMock = mockFetch({ id: 'user-one', displayName: '', isAdmin: false });
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('WebSocket', class {
      close() {}
      send() {}
      addEventListener() {}
      removeEventListener() {}
    });

    render(App);

    const input = await screen.findByLabelText('Display name');
    await fireEvent.input(input, { target: { value: 'Lin' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => expect(document.title).toBe('Planning Circle'));
    expect(screen.queryByText('Opened Planning Circle.')).toBeNull();
    expect(fetchMock).toHaveBeenCalledWith('/api/me', expect.objectContaining({ method: 'PATCH' }));
    expect(fetchMock).toHaveBeenCalledWith('/api/rooms/room-one', expect.anything());
  });

  it('opens the linked room when the name gate is submitted with Enter', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    vi.stubGlobal('fetch', mockFetch({ id: 'user-one', displayName: '', isAdmin: false }));
    vi.stubGlobal('WebSocket', class {
      close() {}
      send() {}
      addEventListener() {}
      removeEventListener() {}
    });

    render(App);

    const input = await screen.findByLabelText('Display name');
    await fireEvent.input(input, { target: { value: 'Lin' } });
    await fireEvent.keyDown(input, { key: 'Enter' });

    await waitFor(() => expect(document.title).toBe('Planning Circle'));
  });

  it('shows a removed-room page immediately when the websocket sends a kick event', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    vi.stubGlobal('fetch', mockFetch({ id: 'user-one', displayName: 'Grace', isAdmin: false }));
    vi.stubGlobal('WebSocket', TestWebSocket);

    render(App);

    await waitFor(() => expect(document.title).toBe('Planning Circle'));
    await waitFor(() => expect(TestWebSocket.sockets).toHaveLength(1));

    TestWebSocket.sockets[0].receive({
      type: 'room.kicked',
      roomId: 'room-one',
      code: 'removed',
      message: "You've been removed from that room."
    });

    await screen.findByRole('heading', { name: "You've been removed from that room" });
    expect(screen.queryByText('Planning Circle')).toBeNull();
    expect(window.location.search).toBe('');
    expect(TestWebSocket.sockets).toHaveLength(1);
  });

  it('shows room retention controls to admin participants', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    vi.stubGlobal('fetch', mockFetch(
      { id: 'admin-one', displayName: 'Ada', isAdmin: true },
      {
        ...roomSnapshot,
        room: {
          ...roomSnapshot.room,
          expiresAt: '2026-05-28T00:00:00Z',
          neverExpires: false
        },
        caller: { userId: 'admin-one', role: 'participant', isAdmin: true }
      }
    ));
    vi.stubGlobal('WebSocket', TestWebSocket);

    render(App);

    await waitFor(() => expect(document.title).toBe('Planning Circle'));
    await fireEvent.click(screen.getByRole('button', { name: /Cache/ }));

    expect(await screen.findByText('Room survival')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Never expire' })).toBeTruthy();
  });

  it('lets moderators toggle participant audio grants from participant cards', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    vi.stubGlobal('fetch', mockFetch(
      { id: 'mod-one', displayName: 'Ada', isAdmin: false },
      {
        ...roomSnapshot,
        caller: { userId: 'mod-one', role: 'mod', isAdmin: false },
        participants: [
          { userId: 'mod-one', displayName: 'Ada', role: 'mod', displayOrder: 0, isOnline: true, allowAudioUpload: false, allowAudioControl: false },
          { userId: 'participant-one', displayName: 'Grace', role: 'participant', displayOrder: 1, isOnline: true, allowAudioUpload: false, allowAudioControl: true }
        ]
      }
    ));
    vi.stubGlobal('WebSocket', TestWebSocket);

    render(App);

    await waitFor(() => expect(TestWebSocket.sockets).toHaveLength(1));
    await fireEvent.click(screen.getByRole('button', { name: /Participants/ }));
    await fireEvent.click(await screen.findByRole('button', { name: "Grant Grace audio upload" }));
    await fireEvent.click(screen.getByRole('button', { name: "Revoke Grace audio control" }));

    expect(TestWebSocket.sockets[0].sent.map((message) => JSON.parse(message))).toEqual(expect.arrayContaining([
      expect.objectContaining({
        type: 'people.audioPermission',
        payload: { userId: 'participant-one', allowAudioUpload: true }
      }),
      expect.objectContaining({
        type: 'people.audioPermission',
        payload: { userId: 'participant-one', allowAudioControl: false }
      })
    ]));
  });

  it('hides per-participant audio grant buttons when room-wide settings grant access', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    vi.stubGlobal('fetch', mockFetch(
      { id: 'mod-one', displayName: 'Ada', isAdmin: false },
      {
        ...roomSnapshot,
        room: {
          ...roomSnapshot.room,
          allowAudienceAudioUpload: true,
          allowAudienceAudioControl: true
        },
        caller: { userId: 'mod-one', role: 'mod', isAdmin: false },
        participants: [
          { userId: 'mod-one', displayName: 'Ada', role: 'mod', displayOrder: 0, isOnline: true, allowAudioUpload: false, allowAudioControl: false },
          { userId: 'participant-one', displayName: 'Grace', role: 'participant', displayOrder: 1, isOnline: true, allowAudioUpload: false, allowAudioControl: false }
        ]
      }
    ));
    vi.stubGlobal('WebSocket', TestWebSocket);

    render(App);

    await waitFor(() => expect(document.title).toBe('Planning Circle'));
    await fireEvent.click(screen.getByRole('button', { name: /Participants/ }));

    expect(screen.queryByRole('button', { name: /Grace audio upload/ })).toBeNull();
    expect(screen.queryByRole('button', { name: /Grace audio control/ })).toBeNull();
  });
});
