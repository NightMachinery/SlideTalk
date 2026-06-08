import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import App from './App.svelte';
import { audioCacheStats, hiddenUploaderDisplayName, listCachedAudio } from './lib/audioCache';

vi.mock('./lib/audioCache', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./lib/audioCache')>();
  return {
    ...actual,
    audioCacheStats: vi.fn(async () => ({ entries: 0, bytes: 0 })),
    slideCacheStats: vi.fn(async () => ({ entries: 0, bytes: 0 })),
    listCachedAudio: vi.fn(async () => []),
    gcAudioCache: vi.fn(async () => {}),
    gcSlideCache: vi.fn(async () => {})
  };
});

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

  open() {
    this.readyState = TestWebSocket.OPEN;
    this.onopen?.(new Event('open'));
  }

  disconnect() {
    this.readyState = TestWebSocket.CLOSED;
    this.onclose?.(new CloseEvent('close'));
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

function blobResponse(body: Blob, status = 200) {
  return new Response(body, { status, headers: { 'Content-Type': body.type || 'application/octet-stream', 'Content-Length': String(body.size) } });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve;
    reject = promiseReject;
  });
  return { promise, resolve, reject };
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
    if (url === '/api/uploads/preflight') return response({ ok: true });
    if (url === '/api/rooms/room-one/audio/restored-track' && method === 'PATCH') return new Response(null, { status: 204 });
    if (url === '/api/rooms/room-one') return response(roomDetails);
    if (url === '/api/rooms/room-one/snapshot') return response(snapshot);
    if (url === '/api/rooms/room-one/ws-ticket') return response({ ticket: 'ticket-one', expiresAt: '2026-05-21T00:01:00Z' });

    throw new Error(`Unhandled request: ${method} ${url}`);
  });
}

class TestUploadRequest {
  static requests: TestUploadRequest[] = [];
  static responses: { status: number; body: unknown }[] = [];
  upload = {
    onprogress: null as ((event: ProgressEvent) => void) | null
  };
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  status = 201;
  responseText = JSON.stringify({
    id: 'restored-track',
    sha256: 'restored-sha',
    originalName: 'restored.mp3',
    title: 'restored.mp3',
    metadataTitle: '',
    mimeType: 'audio/mpeg',
    sizeBytes: 3,
    durationSeconds: 0,
    hasCover: false,
    uploadedByUserId: 'mod-one',
    alreadyUploaded: false,
    missing: false
  });
  method = '';
  url = '';
  headers: Record<string, string> = {};
  body: FormData | null = null;

  constructor() {
    TestUploadRequest.requests.push(this);
    const response = TestUploadRequest.responses.shift();
    if (response) {
      this.status = response.status;
      this.responseText = JSON.stringify(response.body);
    }
  }

  open(method: string, url: string) {
    this.method = method;
    this.url = url;
  }

  setRequestHeader(name: string, value: string) {
    this.headers[name] = value;
  }

  send(body: FormData) {
    this.body = body;
    this.upload.onprogress?.({ lengthComputable: true, loaded: 1, total: 1 } as ProgressEvent);
    this.onload?.();
  }
}

async function firePointer(target: EventTarget, type: string, init: { pointerType: string; clientY: number; pointerId?: number }) {
  const event = new Event(type, { bubbles: true, cancelable: true });
  Object.defineProperty(event, 'pointerType', { value: init.pointerType });
  Object.defineProperty(event, 'clientY', { value: init.clientY });
  Object.defineProperty(event, 'pointerId', { value: init.pointerId ?? 1 });
  await fireEvent(target, event);
}

describe('App landing polish', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    vi.mocked(audioCacheStats).mockResolvedValue({ entries: 0, bytes: 0 });
    vi.mocked(listCachedAudio).mockResolvedValue([]);
    TestUploadRequest.requests = [];
    TestUploadRequest.responses = [];
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

  it('lets moderators restore cached audio into the current room', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    vi.mocked(listCachedAudio).mockResolvedValue([
      {
        sha256: 'cached-sha',
        blob: new Blob(['ID3'], { type: 'audio/mpeg' }),
        mimeType: 'audio/mpeg',
        originalName: 'cached-song.mp3',
        uploaderDisplayName: 'Grace',
        sizeBytes: 3,
        lastAccessedAt: 1,
        createdAt: 1
      }
    ]);
    vi.mocked(audioCacheStats).mockResolvedValue({ entries: 1, bytes: 3 });
    vi.stubGlobal('fetch', mockFetch(
      { id: 'mod-one', displayName: 'Ada', isAdmin: false },
      {
        ...roomSnapshot,
        caller: { userId: 'mod-one', role: 'mod', isAdmin: false }
      }
    ));
    vi.stubGlobal('WebSocket', TestWebSocket);
    vi.stubGlobal('XMLHttpRequest', TestUploadRequest);
    localStorage.setItem('slidetalk.authToken', 'token-one');

    render(App);

    await waitFor(() => expect(document.title).toBe('Planning Circle'));
    await fireEvent.click(screen.getByRole('button', { name: /Cache/ }));
    await fireEvent.click(await screen.findByRole('button', { name: 'Restore cached audio' }));

    await screen.findByText('Restored 1 cached audio file. Skipped 0. Failed 0.');
    expect(TestUploadRequest.requests).toHaveLength(1);
    const request = TestUploadRequest.requests[0];
    expect(request.method).toBe('POST');
    expect(request.url).toBe('/api/rooms/room-one/audio');
    expect(request.headers.Authorization).toBe('Bearer token-one');
    expect(request.body?.get('sha256')).toBe('cached-sha');
    expect(request.body?.get('originalName')).toBe('cached-song.mp3');
    const file = request.body?.get('file') as File;
    expect(file.name).toBe('cached-song.mp3');
    expect(file.type).toBe('audio/mpeg');
    expect(fetch).toHaveBeenCalledWith('/api/rooms/room-one/audio/restored-track', expect.objectContaining({
      method: 'PATCH',
      body: JSON.stringify({ uploaderDisplayName: 'Grace' })
    }));
  });

  it('keeps uploader hidden when restoring legacy cached audio without uploader metadata', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    vi.mocked(listCachedAudio).mockResolvedValue([
      {
        sha256: 'cached-sha',
        blob: new Blob(['ID3'], { type: 'audio/mpeg' }),
        mimeType: 'audio/mpeg',
        originalName: 'cached-song.mp3',
        sizeBytes: 3,
        lastAccessedAt: 1,
        createdAt: 1
      }
    ]);
    vi.mocked(audioCacheStats).mockResolvedValue({ entries: 1, bytes: 3 });
    vi.stubGlobal('fetch', mockFetch(
      { id: 'mod-one', displayName: 'Ada', isAdmin: false },
      {
        ...roomSnapshot,
        caller: { userId: 'mod-one', role: 'mod', isAdmin: false }
      }
    ));
    vi.stubGlobal('WebSocket', TestWebSocket);
    vi.stubGlobal('XMLHttpRequest', TestUploadRequest);
    localStorage.setItem('slidetalk.authToken', 'token-one');

    render(App);

    await waitFor(() => expect(document.title).toBe('Planning Circle'));
    await fireEvent.click(screen.getByRole('button', { name: /Cache/ }));
    await fireEvent.click(await screen.findByRole('button', { name: 'Restore cached audio' }));

    await screen.findByText('Restored 1 cached audio file. Skipped 0. Failed 0.');
    expect(fetch).toHaveBeenCalledWith('/api/rooms/room-one/audio/restored-track', expect.objectContaining({
      method: 'PATCH',
      body: JSON.stringify({ uploaderDisplayName: hiddenUploaderDisplayName })
    }));
  });

  it('skips cached audio already present in the current room', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    vi.mocked(listCachedAudio).mockResolvedValue([
      {
        sha256: 'cached-sha',
        blob: new Blob(['ID3'], { type: 'audio/mpeg' }),
        mimeType: 'audio/mpeg',
        originalName: 'cached-song.mp3',
        uploaderDisplayName: 'Grace',
        sizeBytes: 3,
        lastAccessedAt: 1,
        createdAt: 1
      }
    ]);
    vi.mocked(audioCacheStats).mockResolvedValue({ entries: 1, bytes: 3 });
    const fetchMock = mockFetch(
      { id: 'mod-one', displayName: 'Ada', isAdmin: false },
      {
        ...roomSnapshot,
        caller: { userId: 'mod-one', role: 'mod', isAdmin: false },
        audio: {
          tracks: [
            {
              id: 'existing-track',
              sha256: 'cached-sha',
              originalName: 'cached-song.mp3',
              title: 'cached-song.mp3',
              metadataTitle: '',
              mimeType: 'audio/mpeg',
              sizeBytes: 3,
              durationSeconds: 0,
              hasCover: false,
              uploadedByUserId: 'grace-one',
              uploadedByName: 'Grace',
              uploaderDisplayName: '',
              displayOrder: 0,
              missing: false,
              starredByCaller: false
            }
          ]
        }
      }
    );
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('WebSocket', TestWebSocket);
    vi.stubGlobal('XMLHttpRequest', TestUploadRequest);
    localStorage.setItem('slidetalk.authToken', 'token-one');

    render(App);

    await waitFor(() => expect(document.title).toBe('Planning Circle'));
    await fireEvent.click(screen.getByRole('button', { name: /Cache/ }));
    await fireEvent.click(await screen.findByRole('button', { name: 'Restore cached audio' }));

    await screen.findByText('Restored 0 cached audio files. Skipped 1. Failed 0.');
    expect(TestUploadRequest.requests).toHaveLength(0);
    expect(fetchMock.mock.calls.some(([url]) => url.toString() === '/api/uploads/preflight')).toBe(false);
    expect(fetchMock.mock.calls.some(([url, init]) => url.toString() === '/api/rooms/room-one/audio/restored-track' && init?.method === 'PATCH')).toBe(false);
  });

  it('shows the cached audio restore total in the busy upload button', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    vi.mocked(listCachedAudio).mockResolvedValue(Array.from({ length: 13 }, (_, index) => ({
      sha256: `cached-sha-${index}`,
      blob: new Blob(['ID3'], { type: 'audio/mpeg' }),
      mimeType: 'audio/mpeg',
      originalName: `cached-song-${index}.mp3`,
      uploaderDisplayName: 'Grace',
      sizeBytes: 3,
      lastAccessedAt: 1,
      createdAt: 1
    })));
    vi.mocked(audioCacheStats).mockResolvedValue({ entries: 13, bytes: 39 });
    const preflight = deferred<Response>();
    let preflightReleased = false;
    const audioSnapshot = {
      ...roomSnapshot,
      room: {
        ...roomSnapshot.room,
        roomMode: 'audio'
      },
      caller: { userId: 'mod-one', role: 'mod', isAdmin: false }
    };
    const fetchMock = mockFetch(
      { id: 'mod-one', displayName: 'Ada', isAdmin: false },
      audioSnapshot
    );
    fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (input.toString() === '/api/uploads/preflight') return preflightReleased ? response({ ok: true }) : preflight.promise;
      return mockFetch(
        { id: 'mod-one', displayName: 'Ada', isAdmin: false },
        audioSnapshot
      )(input, init);
    });
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('WebSocket', TestWebSocket);
    vi.stubGlobal('XMLHttpRequest', TestUploadRequest);
    localStorage.setItem('slidetalk.authToken', 'token-one');

    render(App);

    await waitFor(() => expect(document.title).toBe('Planning Circle'));
    await fireEvent.click(screen.getByRole('button', { name: /Cache/ }));
    await fireEvent.click(await screen.findByRole('button', { name: 'Restore cached audio' }));

    await waitFor(() => expect(screen.queryAllByRole('button', { name: 'Working 1/13' }).length).toBeGreaterThan(0));
    expect(screen.queryAllByRole('button', { name: 'Working 1/0' })).toHaveLength(0);
    preflightReleased = true;
    preflight.resolve(response({ ok: true }));
    await screen.findByText('Restored 13 cached audio files. Skipped 0. Failed 0.');
  });

  it('shows cached audio restore upload errors with file names', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    vi.mocked(listCachedAudio).mockResolvedValue([
      {
        sha256: 'cached-sha',
        blob: new Blob(['ID3'], { type: 'audio/mpeg' }),
        mimeType: 'audio/mpeg',
        originalName: 'cached-song.mp3',
        sizeBytes: 3,
        lastAccessedAt: 1,
        createdAt: 1
      }
    ]);
    vi.mocked(audioCacheStats).mockResolvedValue({ entries: 1, bytes: 3 });
    TestUploadRequest.responses = [{ status: 400, body: { detail: 'audio hash does not match uploaded file' } }];
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
    vi.stubGlobal('fetch', mockFetch(
      { id: 'mod-one', displayName: 'Ada', isAdmin: false },
      {
        ...roomSnapshot,
        caller: { userId: 'mod-one', role: 'mod', isAdmin: false }
      }
    ));
    vi.stubGlobal('WebSocket', TestWebSocket);
    vi.stubGlobal('XMLHttpRequest', TestUploadRequest);

    render(App);

    await waitFor(() => expect(document.title).toBe('Planning Circle'));
    await fireEvent.click(screen.getByRole('button', { name: /Cache/ }));
    await fireEvent.click(await screen.findByRole('button', { name: 'Restore cached audio' }));

    await screen.findByText('Restored 0 cached audio files. Skipped 0. Failed 1. cached-song.mp3: audio hash does not match uploaded file');
    expect(consoleError).toHaveBeenCalledWith('Cached audio restore failed', expect.objectContaining({
      sha256: 'cached-sha',
      originalName: 'cached-song.mp3',
      error: expect.any(Error)
    }));
  });

  it('stops cached audio restore before uploading when the server is out of space', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    vi.mocked(listCachedAudio).mockResolvedValue([
      {
        sha256: 'cached-sha-one',
        blob: new Blob(['ID3'], { type: 'audio/mpeg' }),
        mimeType: 'audio/mpeg',
        originalName: 'first.mp3',
        sizeBytes: 3,
        lastAccessedAt: 1,
        createdAt: 1
      },
      {
        sha256: 'cached-sha-two',
        blob: new Blob(['ID3again'], { type: 'audio/mpeg' }),
        mimeType: 'audio/mpeg',
        originalName: 'second.mp3',
        sizeBytes: 8,
        lastAccessedAt: 2,
        createdAt: 2
      }
    ]);
    vi.mocked(audioCacheStats).mockResolvedValue({ entries: 2, bytes: 11 });
    const baseFetch = mockFetch(
      { id: 'mod-one', displayName: 'Ada', isAdmin: false },
      {
        ...roomSnapshot,
        caller: { userId: 'mod-one', role: 'mod', isAdmin: false }
      }
    );
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (input.toString() === '/api/uploads/preflight') {
        return new Response(
          JSON.stringify({ detail: 'The server does not have enough free space. All uploads have been disabled. Contact the server admin to increase storage.' }),
          { status: 400, headers: { 'Content-Type': 'application/problem+json' } }
        );
      }
      return baseFetch(input, init);
    });
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('WebSocket', TestWebSocket);
    vi.stubGlobal('XMLHttpRequest', TestUploadRequest);

    render(App);

    await waitFor(() => expect(document.title).toBe('Planning Circle'));
    await fireEvent.click(screen.getByRole('button', { name: /Cache/ }));
    await fireEvent.click(await screen.findByRole('button', { name: 'Restore cached audio' }));

    await screen.findByText('The server does not have enough free space. All uploads have been disabled. Contact the server admin to increase storage.');
    expect(TestUploadRequest.requests).toHaveLength(0);
    expect(fetchMock).toHaveBeenCalledWith('/api/uploads/preflight', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ sizeBytes: 3 })
    }));
  });

  it('does not show cached audio restore to participants with upload grants', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    vi.mocked(listCachedAudio).mockResolvedValue([
      {
        sha256: 'cached-sha',
        blob: new Blob(['ID3'], { type: 'audio/mpeg' }),
        mimeType: 'audio/mpeg',
        originalName: 'cached-song.mp3',
        sizeBytes: 3,
        lastAccessedAt: 1,
        createdAt: 1
      }
    ]);
    vi.mocked(audioCacheStats).mockResolvedValue({ entries: 1, bytes: 3 });
    vi.stubGlobal('fetch', mockFetch(
      { id: 'participant-one', displayName: 'Grace', isAdmin: false },
      {
        ...roomSnapshot,
        room: {
          ...roomSnapshot.room,
          allowAudienceAudioUpload: true
        },
        caller: { userId: 'participant-one', role: 'participant', isAdmin: false },
        participants: [
          { userId: 'participant-one', displayName: 'Grace', role: 'participant', displayOrder: 0, isOnline: true, allowAudioUpload: true, allowAudioControl: false }
        ]
      }
    ));
    vi.stubGlobal('WebSocket', TestWebSocket);

    render(App);

    await waitFor(() => expect(document.title).toBe('Planning Circle'));
    await fireEvent.click(screen.getByRole('button', { name: /Cache/ }));

    await screen.findByLabelText('Local cache');
    expect(screen.queryByRole('button', { name: 'Restore cached audio' })).toBeNull();
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

  it('dedupes offline command notifications until realtime reconnects', async () => {
    vi.spyOn(crypto, 'randomUUID').mockReturnValue('request-one');
    window.history.replaceState({}, '', '/?room=room-one');
    vi.stubGlobal('fetch', mockFetch(
      { id: 'mod-one', displayName: 'Ada', isAdmin: false },
      {
        ...roomSnapshot,
        caller: { userId: 'mod-one', role: 'mod', isAdmin: false },
        participants: [
          { userId: 'mod-one', displayName: 'Ada', role: 'mod', displayOrder: 0, isOnline: true, allowAudioUpload: false, allowAudioControl: false },
          { userId: 'participant-one', displayName: 'Grace', role: 'participant', displayOrder: 1, isOnline: true, allowAudioUpload: false, allowAudioControl: false }
        ]
      }
    ));
    vi.stubGlobal('WebSocket', TestWebSocket);

    render(App);

    await waitFor(() => expect(TestWebSocket.sockets).toHaveLength(1));
    TestWebSocket.sockets[0].disconnect();
    await fireEvent.click(screen.getByRole('button', { name: /Participants/ }));
    const moveDown = (await screen.findAllByRole('button', { name: 'Move down' })).find((button) => !button.hasAttribute('disabled'));
    expect(moveDown).toBeTruthy();
    await fireEvent.click(moveDown as HTMLElement);
    await fireEvent.click(moveDown as HTMLElement);

    expect(screen.getAllByText('Live connection is offline. Changes will work after it reconnects.')).toHaveLength(1);

    await waitFor(() => expect(TestWebSocket.sockets.length).toBeGreaterThan(1));
    TestWebSocket.sockets[1].open();
    TestWebSocket.sockets[1].disconnect();
    await fireEvent.click(moveDown as HTMLElement);

    expect(screen.getAllByText('Live connection is offline. Changes will work after it reconnects.')).toHaveLength(2);
  });

  it('shows only user-initiated realtime command errors', async () => {
    vi.spyOn(crypto, 'randomUUID').mockReturnValue('request-one');
    window.history.replaceState({}, '', '/?room=room-one');
    vi.stubGlobal('fetch', mockFetch(
      { id: 'mod-one', displayName: 'Ada', isAdmin: false },
      {
        ...roomSnapshot,
        caller: { userId: 'mod-one', role: 'mod', isAdmin: false },
        participants: [
          { userId: 'mod-one', displayName: 'Ada', role: 'mod', displayOrder: 0, isOnline: true, allowAudioUpload: false, allowAudioControl: false },
          { userId: 'participant-one', displayName: 'Grace', role: 'participant', displayOrder: 1, isOnline: true, allowAudioUpload: false, allowAudioControl: false }
        ]
      }
    ));
    vi.stubGlobal('WebSocket', TestWebSocket);

    render(App);

    await waitFor(() => expect(TestWebSocket.sockets).toHaveLength(1));
    await fireEvent.click(screen.getByRole('button', { name: /Participants/ }));
    const moveDown = (await screen.findAllByRole('button', { name: 'Move down' })).find((button) => !button.hasAttribute('disabled'));
    expect(moveDown).toBeTruthy();
    await fireEvent.click(moveDown as HTMLElement);

    TestWebSocket.sockets[0].receive({ type: 'error', requestId: 'background-request', message: 'Background failed.' });
    expect(screen.queryByText('Background failed.')).toBeNull();

    TestWebSocket.sockets[0].receive({ type: 'error', requestId: 'request-one', message: 'Move failed.' });
    expect(await screen.findByText('Move failed.')).toBeTruthy();
  });

  it('shows connection status in the active room top bar', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    vi.stubGlobal('fetch', mockFetch({ id: 'user-one', displayName: 'Grace', isAdmin: false }));
    vi.stubGlobal('WebSocket', TestWebSocket);

    render(App);

    await waitFor(() => expect(TestWebSocket.sockets.length).toBeGreaterThan(0));
    expect(await screen.findByLabelText('Live connection connecting')).toBeTruthy();
    const socket = TestWebSocket.sockets.at(-1);
    expect(socket).toBeTruthy();
    socket?.open();
    expect(await screen.findByLabelText('Live connection connected')).toBeTruthy();
    socket?.disconnect();
    expect(await screen.findByLabelText('Live connection disconnected')).toBeTruthy();
  });


  it('lets members use local unsynced audio without shared control', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined);
    const audioSnapshot = {
      ...roomSnapshot,
      room: {
        ...roomSnapshot.room,
        roomMode: 'audio',
        allowAudienceAudioControl: false
      },
      caller: { userId: 'participant-one', role: 'participant', isAdmin: false },
      participants: [
        { userId: 'participant-one', displayName: 'Grace', role: 'participant', displayOrder: 0, isOnline: true, allowAudioUpload: false, allowAudioControl: false }
      ],
      audio: {
        tracks: [
          {
            id: 'track-one',
            sha256: 'track-one-sha',
            originalName: 'track-one.mp3',
            title: 'Track one',
            metadataTitle: '',
            mimeType: 'audio/mpeg',
            sizeBytes: 7,
            durationSeconds: 30,
            hasCover: false,
            uploadedByUserId: 'mod-one',
            uploadedByName: 'Ada',
            uploaderDisplayName: '',
            displayOrder: 0,
            missing: false,
            starredByCaller: false
          }
        ],
        currentTrackId: 'track-one',
        nextTrackId: '',
        state: 'paused',
        positionSeconds: 0,
        startedAt: null,
        serverNow: '2026-05-21T00:00:00Z',
        playbackMode: 'stop'
      }
    };
    const fetchMock = mockFetch({ id: 'participant-one', displayName: 'Grace', isAdmin: false }, audioSnapshot);
    fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = input.toString();
      if (url === '/api/rooms/room-one/audio/track-one/download-link') return response({ url: '/download/track-one' });
      if (url === '/download/track-one') return blobResponse(new Blob(['audio'], { type: 'audio/mpeg' }));
      return mockFetch({ id: 'participant-one', displayName: 'Grace', isAdmin: false }, audioSnapshot)(input, init);
    });
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('WebSocket', TestWebSocket);

    render(App);

    await waitFor(() => expect(TestWebSocket.sockets).toHaveLength(1));
    TestWebSocket.sockets[0].open();
    const playButton = await screen.findByRole('button', { name: 'Play' });
    expect(playButton.hasAttribute('disabled')).toBe(true);
    expect(playButton.textContent?.trim()).toBe('');
    expect(screen.queryByText('Local audio mode: only you hear these controls.')).toBeNull();

    const priorOfflineToasts = screen.queryAllByText('Live connection is offline. Changes will work after it reconnects.').length;
    await fireEvent.click(screen.getByLabelText('Local mode (unsynced)'));
    await waitFor(() => expect(TestWebSocket.sockets[0].sent.some((message) => JSON.parse(message).type === 'presence.audioLocalMode')).toBe(true));
    await waitFor(() => expect(playButton.hasAttribute('disabled')).toBe(false));
    await fireEvent.click(playButton);
    await fireEvent.click(screen.getByRole('button', { name: 'Next track' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Previous track' }));

    const sentTypes = TestWebSocket.sockets[0].sent.map((message) => JSON.parse(message).type);
    expect(sentTypes).toContain('presence.audioLocalMode');
    expect(sentTypes).not.toContain('audio.play');
    expect(screen.queryAllByText('Live connection is offline. Changes will work after it reconnects.')).toHaveLength(priorOfflineToasts);
  });


  it('shows current audio track and starred filter visual states', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    const scrollIntoView = vi.fn();
    Object.defineProperty(Element.prototype, 'scrollIntoView', { value: scrollIntoView, configurable: true });
    vi.stubGlobal('fetch', mockFetch(
      { id: 'mod-one', displayName: 'Ada', isAdmin: false },
      {
        ...roomSnapshot,
        room: { ...roomSnapshot.room, roomMode: 'audio' },
        caller: { userId: 'mod-one', role: 'mod', isAdmin: false },
        audio: {
          tracks: [
            {
              id: 'track-one', sha256: 'track-one-sha', originalName: 'track-one.mp3', title: 'Track one', metadataTitle: '', mimeType: 'audio/mpeg', sizeBytes: 7, durationSeconds: 30, hasCover: false, uploadedByUserId: 'mod-one', uploadedByName: 'Ada', uploaderDisplayName: '', displayOrder: 0, missing: false, starredByCaller: true
            }
          ],
          currentTrackId: 'track-one', nextTrackId: '', state: 'paused', positionSeconds: 0, startedAt: null, serverNow: '2026-05-21T00:00:00Z', playbackMode: 'stop'
        }
      }
    ));
    vi.stubGlobal('WebSocket', TestWebSocket);

    render(App);

    const starred = await screen.findByRole('button', { name: 'Starred' });
    expect(starred.classList.contains('inactive-filter')).toBe(true);
    expect(starred.classList.contains('active-filter')).toBe(false);

    await fireEvent.click(starred);
    expect(starred.classList.contains('active-filter')).toBe(true);
    expect(starred.classList.contains('inactive-filter')).toBe(false);

    await fireEvent.click(screen.getByRole('button', { name: 'Show current track' }));
    expect(scrollIntoView).toHaveBeenCalled();
  });


  it('uses keyboard media keys for local audio playback without shared commands', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined);
    const audioSnapshot = {
      ...roomSnapshot,
      room: { ...roomSnapshot.room, roomMode: 'audio', allowAudienceAudioControl: false },
      caller: { userId: 'participant-one', role: 'participant', isAdmin: false },
      participants: [
        { userId: 'participant-one', displayName: 'Grace', role: 'participant', displayOrder: 0, isOnline: true, allowAudioUpload: false, allowAudioControl: false }
      ],
      audio: {
        tracks: [
          { id: 'track-one', sha256: 'track-one-sha', originalName: 'track-one.mp3', title: 'Track one', metadataTitle: '', mimeType: 'audio/mpeg', sizeBytes: 7, durationSeconds: 30, hasCover: false, uploadedByUserId: 'mod-one', uploadedByName: 'Ada', uploaderDisplayName: '', displayOrder: 0, missing: false, starredByCaller: false },
          { id: 'track-two', sha256: 'track-two-sha', originalName: 'track-two.mp3', title: 'Track two', metadataTitle: '', mimeType: 'audio/mpeg', sizeBytes: 8, durationSeconds: 40, hasCover: false, uploadedByUserId: 'mod-one', uploadedByName: 'Ada', uploaderDisplayName: '', displayOrder: 1, missing: false, starredByCaller: false }
        ],
        currentTrackId: 'track-one',
        nextTrackId: 'track-two',
        state: 'paused',
        positionSeconds: 0,
        startedAt: null,
        serverNow: '2026-05-21T00:00:00Z',
        playbackMode: 'next'
      }
    };
    const fetchMock = mockFetch({ id: 'participant-one', displayName: 'Grace', isAdmin: false }, audioSnapshot);
    fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = input.toString();
      if (url === '/api/rooms/room-one/audio/track-one/download-link') return response({ url: '/download/track-one' });
      if (url === '/api/rooms/room-one/audio/track-two/download-link') return response({ url: '/download/track-two' });
      if (url === '/download/track-one') return blobResponse(new Blob(['one'], { type: 'audio/mpeg' }));
      if (url === '/download/track-two') return blobResponse(new Blob(['two'], { type: 'audio/mpeg' }));
      return mockFetch({ id: 'participant-one', displayName: 'Grace', isAdmin: false }, audioSnapshot)(input, init);
    });
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('WebSocket', TestWebSocket);

    render(App);

    await waitFor(() => expect(TestWebSocket.sockets).toHaveLength(1));
    TestWebSocket.sockets[0].open();
    await fireEvent.click(await screen.findByLabelText('Local mode (unsynced)'));
    expect(document.querySelector('.audio-mini-copy strong')?.textContent).toBe('Track one');
    await fireEvent.keyDown(window, { key: 'MediaPlayPause' });
    await fireEvent.keyDown(window, { key: 'MediaTrackNext' });

    await waitFor(() => expect(document.querySelector('.audio-mini-copy strong')?.textContent).toBe('Track two'));
    const sentTypes = TestWebSocket.sockets[0].sent.map((message) => JSON.parse(message).type);
    expect(sentTypes).toContain('presence.audioLocalMode');
    expect(sentTypes).not.toContain('audio.play');
  });

  it('uses keyboard media keys for synced audio when caller can control audio', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    const audioSnapshot = {
      ...roomSnapshot,
      room: { ...roomSnapshot.room, roomMode: 'audio' },
      caller: { userId: 'mod-one', role: 'mod', isAdmin: false },
      audio: {
        tracks: [
          { id: 'track-one', sha256: 'track-one-sha', originalName: 'track-one.mp3', title: 'Track one', metadataTitle: '', mimeType: 'audio/mpeg', sizeBytes: 7, durationSeconds: 30, hasCover: false, uploadedByUserId: 'mod-one', uploadedByName: 'Ada', uploaderDisplayName: '', displayOrder: 0, missing: false, starredByCaller: false },
          { id: 'track-two', sha256: 'track-two-sha', originalName: 'track-two.mp3', title: 'Track two', metadataTitle: '', mimeType: 'audio/mpeg', sizeBytes: 8, durationSeconds: 40, hasCover: false, uploadedByUserId: 'mod-one', uploadedByName: 'Ada', uploaderDisplayName: '', displayOrder: 1, missing: false, starredByCaller: false }
        ],
        currentTrackId: 'track-one',
        nextTrackId: 'track-two',
        state: 'paused',
        positionSeconds: 0,
        startedAt: null,
        serverNow: '2026-05-21T00:00:00Z',
        playbackMode: 'next'
      }
    };
    vi.stubGlobal('fetch', mockFetch({ id: 'mod-one', displayName: 'Ada', isAdmin: false }, audioSnapshot));
    vi.stubGlobal('WebSocket', TestWebSocket);
    vi.spyOn(crypto, 'randomUUID').mockReturnValueOnce('play-request').mockReturnValueOnce('next-request');

    render(App);

    await waitFor(() => expect(TestWebSocket.sockets).toHaveLength(1));
    TestWebSocket.sockets[0].open();
    await waitFor(() => expect(screen.getAllByText('Track one').length).toBeGreaterThan(0));
    await fireEvent.keyDown(window, { key: 'MediaPlayPause' });
    await fireEvent.keyDown(window, { key: 'MediaTrackNext' });

    const audioCommands = TestWebSocket.sockets[0].sent.map((message) => JSON.parse(message)).filter((message) => message.type === 'audio.play');
    expect(audioCommands).toEqual([
      expect.objectContaining({ payload: { trackId: 'track-one', positionSeconds: 0 } }),
      expect.objectContaining({ payload: { trackId: 'track-two', positionSeconds: 0 } })
    ]);
  });

  it('shows local audio badges in people lists', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    vi.stubGlobal('fetch', mockFetch(
      { id: 'mod-one', displayName: 'Ada', isAdmin: false },
      {
        ...roomSnapshot,
        caller: { userId: 'mod-one', role: 'mod', isAdmin: false },
        participants: [
          { userId: 'mod-one', displayName: 'Ada', role: 'mod', displayOrder: 0, isOnline: true, allowAudioUpload: false, allowAudioControl: false, audioLocalMode: false },
          { userId: 'participant-one', displayName: 'Grace', role: 'participant', displayOrder: 1, isOnline: true, allowAudioUpload: false, allowAudioControl: false, audioLocalMode: true }
        ],
        observers: [
          { userId: 'observer-one', displayName: 'Lin', role: 'observer', displayOrder: 0, isOnline: true, allowAudioUpload: false, allowAudioControl: false, audioLocalMode: true }
        ]
      }
    ));
    vi.stubGlobal('WebSocket', TestWebSocket);

    render(App);

    await fireEvent.click(await screen.findByRole('button', { name: /Participants/ }));
    await fireEvent.click(await screen.findByRole('button', { name: /Observers/ }));

    expect(screen.getAllByText('Local audio')).toHaveLength(2);
    expect(screen.getAllByLabelText('Using local audio mode, not synced')).toHaveLength(2);
  });

  it('loads and saves per-person local audio volume preferences', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    localStorage.setItem('slidetalk.roomAudioPrefs.v1:room-one:user-one', JSON.stringify({ muted: true, volume: 0.35 }));
    vi.stubGlobal('fetch', mockFetch(
      { id: 'user-one', displayName: 'Grace', isAdmin: false },
      {
        ...roomSnapshot,
        room: {
          ...roomSnapshot.room,
          roomMode: 'audio'
        },
        audio: {
          tracks: [],
          currentTrackId: '',
          state: 'paused',
          positionSeconds: 0,
          startedAt: null,
          serverNow: '2026-05-21T00:00:00Z',
          playbackMode: 'stop'
        }
      }
    ));
    vi.stubGlobal('WebSocket', TestWebSocket);

    render(App);

    await waitFor(() => expect(screen.queryByLabelText('Local audio volume')).toBeNull());
    const muteButton = await screen.findByRole('button', { name: 'Unmute local audio' });
    const volumeButton = screen.getByRole('button', { name: 'Open local audio volume' });
    expect(volumeButton.classList.contains('icon-button')).toBe(true);
    const setPointerCapture = vi.fn();
    const releasePointerCapture = vi.fn();
    Object.defineProperty(muteButton, 'setPointerCapture', { value: setPointerCapture, configurable: true });
    Object.defineProperty(muteButton, 'releasePointerCapture', { value: releasePointerCapture, configurable: true });

    vi.useFakeTimers();
    await firePointer(muteButton, 'pointerdown', { pointerType: 'mouse', clientY: 200, pointerId: 2 });
    vi.advanceTimersByTime(450);
    await vi.runOnlyPendingTimersAsync();
    expect(screen.queryByLabelText('Local audio volume')).toBeNull();

    await firePointer(muteButton, 'pointerdown', { pointerType: 'touch', clientY: 200, pointerId: 7 });
    expect(setPointerCapture).toHaveBeenCalledWith(7);
    vi.advanceTimersByTime(450);
    await vi.runOnlyPendingTimersAsync();
    await firePointer(muteButton, 'pointermove', { pointerType: 'touch', clientY: 176, pointerId: 7 });
    const volume = screen.getByLabelText('Local audio volume') as HTMLInputElement;
    await waitFor(() => expect(volume.value).toBe('55'));
    await firePointer(muteButton, 'pointermove', { pointerType: 'touch', clientY: 224, pointerId: 7 });
    await firePointer(muteButton, 'pointerup', { pointerType: 'touch', clientY: 224, pointerId: 7 });
    await vi.runOnlyPendingTimersAsync();
    vi.useRealTimers();

    const audio = document.querySelector('audio') as HTMLAudioElement;
    await waitFor(() => expect(volume.value).toBe('15'));
    expect(audio.muted).toBe(true);
    expect(audio.volume).toBeCloseTo(0.15);
    expect(releasePointerCapture).toHaveBeenCalledWith(7);

    await fireEvent.input(volume, { target: { value: '62' } });
    await fireEvent.click(muteButton);

    expect(JSON.parse(localStorage.getItem('slidetalk.roomAudioPrefs.v1:room-one:user-one') ?? '{}')).toEqual({ muted: false, volume: 0.62 });
  });

  it('starts caching the server-announced next audio track', async () => {
    window.history.replaceState({}, '', '/?room=room-one');
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined);
    const audioSnapshot = {
      ...roomSnapshot,
      room: {
        ...roomSnapshot.room,
        roomMode: 'audio'
      },
      caller: { userId: 'mod-one', role: 'mod', isAdmin: false },
      audio: {
        tracks: [
            {
              id: 'current-track',
              sha256: 'current-sha',
              originalName: 'current.mp3',
              title: 'Current',
              metadataTitle: '',
              mimeType: 'audio/mpeg',
              sizeBytes: 7,
              durationSeconds: 30,
              hasCover: false,
              uploadedByUserId: 'mod-one',
              uploadedByName: 'Ada',
              uploaderDisplayName: '',
              displayOrder: 0,
              missing: false,
              starredByCaller: false
            },
            {
              id: 'next-track',
              sha256: 'next-sha',
              originalName: 'next.mp3',
              title: 'Next',
              metadataTitle: '',
              mimeType: 'audio/mpeg',
              sizeBytes: 8,
              durationSeconds: 32,
              hasCover: false,
              uploadedByUserId: 'grace-one',
              uploadedByName: 'Grace',
              uploaderDisplayName: '',
              displayOrder: 1,
              missing: false,
              starredByCaller: false
            }
        ],
        currentTrackId: 'current-track',
        nextTrackId: 'next-track',
        state: 'playing',
        positionSeconds: 0,
        startedAt: '2026-05-21T00:00:00Z',
        serverNow: '2026-05-21T00:00:00Z',
        playbackMode: 'next'
      }
    };
    const fetchMock = mockFetch(
      { id: 'mod-one', displayName: 'Ada', isAdmin: false },
      audioSnapshot
    );
    fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = input.toString();
      if (url === '/api/rooms/room-one/audio/current-track/download-link') return response({ url: '/download/current' });
      if (url === '/api/rooms/room-one/audio/next-track/download-link') return response({ url: '/download/next' });
      if (url === '/download/current') return blobResponse(new Blob(['current'], { type: 'audio/mpeg' }));
      if (url === '/download/next') return blobResponse(new Blob(['next'], { type: 'audio/mpeg' }));
      return mockFetch(
        { id: 'mod-one', displayName: 'Ada', isAdmin: false },
        audioSnapshot
      )(input, init);
    });
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('WebSocket', TestWebSocket);
    localStorage.setItem('slidetalk.authToken', 'token-one');

    render(App);

    await waitFor(() => expect(document.title).toBe('Planning Circle'));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/rooms/room-one/audio/next-track/download-link', expect.objectContaining({ method: 'POST' })));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/download/next'));
    expect(fetchMock).toHaveBeenCalledWith('/api/rooms/room-one/audio/current-track/download-link', expect.objectContaining({ method: 'POST' }));
  });
});
