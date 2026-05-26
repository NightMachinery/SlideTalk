import { describe, expect, it, vi } from 'vitest';
import { connectRealtime, normalizeRoomSnapshot, realtimeURL } from './realtime';

class FakeWebSocket extends EventTarget {
  static sockets: FakeWebSocket[] = [];
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 3;
  readonly url: string;
  readyState = FakeWebSocket.CONNECTING;
  sent: string[] = [];

  onopen: ((event: Event) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;

  constructor(url: string) {
    super();
    this.url = url;
    FakeWebSocket.sockets.push(this);
  }

  send(message: string) {
    this.sent.push(message);
  }

  close() {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.(new CloseEvent('close'));
  }

  open() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.(new Event('open'));
  }

  drop() {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.(new CloseEvent('close'));
  }
}

describe('realtimeURL', () => {
  it('uses wss on https and ws on http', () => {
    expect(realtimeURL('https:', 'talk.example', 'ticket 1')).toBe('wss://talk.example/api/ws?ticket=ticket%201');
    expect(realtimeURL('http:', 'talk.example', 'ticket 2')).toBe('ws://talk.example/api/ws?ticket=ticket%202');
  });
});

describe('connectRealtime', () => {
  it('fetches a fresh ticket and reconnects after a dropped socket', async () => {
    vi.useFakeTimers();
    FakeWebSocket.sockets = [];
    const statuses: string[] = [];
    const tickets = ['first-ticket', 'second-ticket'];

    await connectRealtime(
      'room-one',
      () => {},
      (status) => statuses.push(status),
      {
        createTicket: async () => ({ ticket: tickets.shift() ?? 'extra-ticket', expiresAt: new Date().toISOString() }),
        WebSocketClass: FakeWebSocket,
        reconnectDelaysMs: [0]
      }
    );

    expect(FakeWebSocket.sockets).toHaveLength(1);
    expect(FakeWebSocket.sockets[0].url).toContain('first-ticket');
    FakeWebSocket.sockets[0].open();
    FakeWebSocket.sockets[0].drop();

    await vi.runOnlyPendingTimersAsync();
    await Promise.resolve();

    expect(FakeWebSocket.sockets).toHaveLength(2);
    expect(FakeWebSocket.sockets[1].url).toContain('second-ticket');
    FakeWebSocket.sockets[1].open();
    expect(statuses).toEqual(['connected', 'disconnected', 'connecting', 'connected']);
    vi.useRealTimers();
  });

  it('does not send commands until the socket is open', async () => {
    FakeWebSocket.sockets = [];

    const connection = await connectRealtime(
      'room-one',
      () => {},
      () => {},
      {
        createTicket: async () => ({ ticket: 'ticket', expiresAt: new Date().toISOString() }),
        WebSocketClass: FakeWebSocket
      }
    );

    expect(connection.send({ type: 'turn.next' })).toBe(false);
    expect(FakeWebSocket.sockets[0].sent).toHaveLength(0);

    FakeWebSocket.sockets[0].open();
    expect(connection.send({ type: 'turn.next' })).toBe(true);
    expect(FakeWebSocket.sockets[0].sent).toHaveLength(1);
  });
});

describe('normalizeRoomSnapshot', () => {
  it('converts null snapshot collections to empty arrays', () => {
    const snapshot = normalizeRoomSnapshot({
      room: { id: 'room-one', title: 'Live', hasPassword: false, roomMode: 'slides', allowParticipantMarkdown: false, raiseHandMode: 'off', slidePage: 1, sharedNavigationEnabled: true },
      caller: { userId: 'user-one', role: 'mod', isAdmin: false },
      participants: null,
      observers: null,
      currentTurn: { currentSpeakerUserId: '', nextSpeakerUserId: '' },
      timer: { state: 'stopped', durationSeconds: 0, startedAt: null, serverNow: '2026-05-22T00:00:00Z' },
      hands: null,
      slide: null,
      markdown: '',
      markdownUpdatedByUserId: '',
      markdownUpdatedByName: '',
      markdownUpdatedAt: ''
    });

    expect(snapshot.participants).toEqual([]);
    expect(snapshot.observers).toEqual([]);
    expect(snapshot.hands).toEqual([]);
  });

  it('defaults missing room mode to slides', () => {
    const snapshot = normalizeRoomSnapshot({
      room: { id: 'room-one', title: 'Live', hasPassword: false, allowParticipantMarkdown: false, raiseHandMode: 'off', slidePage: 1, sharedNavigationEnabled: true },
      caller: { userId: 'user-one', role: 'mod', isAdmin: false },
      participants: [{ userId: 'user-one', displayName: 'Ada', role: 'participant', displayOrder: 0, isOnline: true }],
      observers: [],
      currentTurn: { currentSpeakerUserId: '', nextSpeakerUserId: '' },
      timer: { state: 'stopped', durationSeconds: 0, startedAt: null, serverNow: '2026-05-22T00:00:00Z' },
      hands: [],
      slide: null,
      markdown: '',
      markdownUpdatedByUserId: '',
      markdownUpdatedByName: '',
      markdownUpdatedAt: ''
    });

    expect(snapshot.room.roomMode).toBe('slides');
    expect(snapshot.room.allowAudienceAudioUpload).toBe(false);
    expect(snapshot.participants[0].allowAudioUpload).toBe(false);
    expect(snapshot.participants[0].allowAudioControl).toBe(false);
  });

  it('preserves slide MIME metadata during normalization', () => {
    const snapshot = normalizeRoomSnapshot({
      room: { id: 'room-one', title: 'Live', hasPassword: false, roomMode: 'slides', allowParticipantMarkdown: false, raiseHandMode: 'off', slidePage: 1, sharedNavigationEnabled: true },
      caller: { userId: 'user-one', role: 'mod', isAdmin: false },
      participants: [],
      observers: [],
      currentTurn: { currentSpeakerUserId: '', nextSpeakerUserId: '' },
      timer: { state: 'stopped', durationSeconds: 0, startedAt: null, serverNow: '2026-05-22T00:00:00Z' },
      hands: [],
      slide: {
        sha256: 'a'.repeat(64),
        originalName: 'diagram.png',
        expiresAt: '2026-05-23T00:00:00Z',
        missing: false,
        mimeType: 'image/png'
      },
      markdown: '',
      markdownUpdatedByUserId: '',
      markdownUpdatedByName: '',
      markdownUpdatedAt: ''
    });

    expect(snapshot.slide?.mimeType).toBe('image/png');
  });

  it('defaults new audio metadata during normalization', () => {
    const snapshot = normalizeRoomSnapshot({
      room: { id: 'room-one', title: 'Live', hasPassword: false, roomMode: 'audio', allowParticipantMarkdown: false, raiseHandMode: 'off', slidePage: 1, sharedNavigationEnabled: true },
      caller: { userId: 'user-one', role: 'mod', isAdmin: false },
      participants: [],
      observers: [],
      currentTurn: { currentSpeakerUserId: '', nextSpeakerUserId: '' },
      timer: { state: 'stopped', durationSeconds: 0, startedAt: null, serverNow: '2026-05-22T00:00:00Z' },
      hands: [],
      slide: null,
      audio: {
        tracks: [{
          id: 'track-one',
          sha256: 'a'.repeat(64),
          originalName: 'song.mp3',
          mimeType: 'audio/mpeg',
          sizeBytes: 12,
          uploadedByUserId: 'user-one',
          displayOrder: 0,
          missing: false
        }],
        currentTrackId: 'track-one'
      },
      markdown: '',
      markdownUpdatedByUserId: '',
      markdownUpdatedByName: '',
      markdownUpdatedAt: ''
    });

    expect(snapshot.audio.tracks[0].title).toBe('song.mp3');
    expect(snapshot.audio.tracks[0].durationSeconds).toBe(0);
    expect(snapshot.audio.tracks[0].hasCover).toBe(false);
  });
});
