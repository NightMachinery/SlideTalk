import { createWSTicket, type WSTicket } from './api';

export type SnapshotMember = {
  userId: string;
  displayName: string;
  role: 'mod' | 'participant' | 'observer';
  displayOrder: number;
};

export type RoomSnapshot = {
  room: {
    id: string;
    title: string;
    hasPassword: boolean;
    roomMode: 'slides' | 'markdown' | 'audio';
    allowParticipantMarkdown: boolean;
    raiseHandMode: 'off' | 'manual' | 'queue';
    slidePage: number;
    sharedNavigationEnabled: boolean;
    allowAudienceAudioUpload: boolean;
    allowAudienceAudioControl: boolean;
  };
  caller: {
    userId: string;
    role: 'mod' | 'participant' | 'observer';
    isAdmin: boolean;
  };
  participants: SnapshotMember[];
  observers: SnapshotMember[];
  currentTurn: {
    currentSpeakerUserId: string;
    nextSpeakerUserId: string;
  };
  timer: {
    state: 'stopped' | 'running';
    durationSeconds: number;
    startedAt: string | null;
    serverNow: string;
  };
  hands: {
    userId: string;
    displayName: string;
    raisedAt: string;
  }[];
  slide: {
    sha256: string;
    originalName: string;
    mimeType: string;
    expiresAt: string;
    missing: boolean;
  } | null;
  audio: {
    tracks: {
      id: string;
      sha256: string;
      originalName: string;
      title: string;
      metadataTitle: string;
      mimeType: string;
      sizeBytes: number;
      durationSeconds: number;
      hasCover: boolean;
      uploadedByUserId: string;
      uploadedByName: string;
      uploaderDisplayName: string;
      displayOrder: number;
      missing: boolean;
    }[];
    currentTrackId: string;
    state: 'paused' | 'playing';
    positionSeconds: number;
    startedAt: string | null;
    serverNow: string;
    playbackMode: 'stop' | 'next' | 'previous' | 'repeat-one' | 'repeat-forward' | 'repeat-backward' | 'shuffle';
  };
  markdown: string;
  markdownUpdatedByUserId: string;
  markdownUpdatedByName: string;
  markdownUpdatedAt: string;
};

export type RealtimeEvent = {
  type: 'room.snapshot' | 'room.kicked' | 'error';
  requestId?: string;
  roomId?: string;
  version?: number;
  code?: string;
  message?: string;
  payload?: RoomSnapshot;
};

type RoomSnapshotWireAudio = Partial<Omit<RoomSnapshot['audio'], 'tracks'>> & {
  tracks?: (Omit<RoomSnapshot['audio']['tracks'][number], 'title' | 'metadataTitle' | 'durationSeconds' | 'hasCover' | 'uploadedByName' | 'uploaderDisplayName'> &
    Partial<Pick<RoomSnapshot['audio']['tracks'][number], 'title' | 'metadataTitle' | 'durationSeconds' | 'hasCover' | 'uploadedByName' | 'uploaderDisplayName'>>)[];
};

type RoomSnapshotWire = Omit<RoomSnapshot, 'participants' | 'observers' | 'hands' | 'audio'> & {
  participants?: SnapshotMember[] | null;
  observers?: SnapshotMember[] | null;
  hands?: RoomSnapshot['hands'] | null;
  audio?: RoomSnapshotWireAudio | null;
};

export function normalizeRoomSnapshot(snapshot: RoomSnapshotWire): RoomSnapshot {
  return {
    ...snapshot,
    room: {
      ...snapshot.room,
      roomMode: snapshot.room.roomMode ?? 'slides',
      allowAudienceAudioUpload: snapshot.room.allowAudienceAudioUpload ?? false,
      allowAudienceAudioControl: snapshot.room.allowAudienceAudioControl ?? false
    },
    participants: snapshot.participants ?? [],
    observers: snapshot.observers ?? [],
    hands: snapshot.hands ?? [],
    audio: {
      tracks: (snapshot.audio?.tracks ?? []).map((track) => ({
        ...track,
        title: track.title || track.originalName || 'Untitled audio',
        metadataTitle: track.metadataTitle ?? '',
        durationSeconds: track.durationSeconds ?? 0,
        hasCover: track.hasCover ?? false,
        uploadedByName: track.uploadedByName ?? '',
        uploaderDisplayName: track.uploaderDisplayName ?? ''
      })),
      currentTrackId: snapshot.audio?.currentTrackId ?? '',
      state: snapshot.audio?.state ?? 'paused',
      positionSeconds: snapshot.audio?.positionSeconds ?? 0,
      startedAt: snapshot.audio?.startedAt ?? null,
      serverNow: snapshot.audio?.serverNow ?? new Date().toISOString(),
      playbackMode: snapshot.audio?.playbackMode ?? 'stop'
    }
  };
}

export type RealtimeCommand =
  | {
      type: 'people.reorder';
      payload: { orderedUserIds: string[]; observerUserIds: string[] };
    }
  | {
      type: 'people.setRole';
      payload: { userId: string; role: 'mod' | 'participant' | 'observer' };
    }
  | {
      type: 'people.kick';
      payload: { userId: string };
    }
  | {
      type: 'turn.next' | 'turn.previous';
    }
  | {
      type: 'turn.setCurrent';
      payload: { userId: string };
    }
  | {
      type: 'timer.start';
      payload: { durationSeconds: number };
    }
  | {
      type: 'timer.stop' | 'timer.reset' | 'hand.raise';
    }
  | {
      type: 'hand.lower';
      payload?: { userId: string };
    }
  | {
      type: 'settings.update';
      payload: {
        raiseHandMode?: 'off' | 'manual' | 'queue';
        sharedNavigationEnabled?: boolean;
        roomMode?: 'slides' | 'markdown' | 'audio';
        allowParticipantMarkdown?: boolean;
        allowAudienceAudioUpload?: boolean;
        allowAudienceAudioControl?: boolean;
      };
    }
  | {
      type: 'slide.navigate';
      payload: { page: number; modSharedNavigationEnabled: boolean };
    }
  | {
      type: 'markdown.update';
      payload: { markdown: string };
    }
  | {
      type: 'audio.play';
      payload: { trackId?: string; positionSeconds: number };
    }
  | {
      type: 'audio.pause' | 'audio.ended';
    }
  | {
      type: 'audio.seek';
      payload: { positionSeconds: number };
    }
  | {
      type: 'audio.select';
      payload: { trackId: string };
    }
  | {
      type: 'audio.reorder';
      payload: { trackIds: string[] };
    }
  | {
      type: 'audio.mode';
      payload: { mode: RoomSnapshot['audio']['playbackMode'] };
    };

export type RealtimeConnection = {
  send(command: RealtimeCommand): boolean;
  close(): void;
};

type WebSocketLike = {
  readyState: number;
  onopen: ((event: Event) => void) | null;
  onclose: ((event: CloseEvent) => void) | null;
  onerror: ((event: Event) => void) | null;
  onmessage: ((event: MessageEvent) => void) | null;
  send(message: string): void;
  close(): void;
};

type RealtimeOptions = {
  createTicket?: (roomId: string) => Promise<WSTicket>;
  WebSocketClass?: new (url: string) => WebSocketLike;
  reconnectDelaysMs?: number[];
};

const defaultReconnectDelaysMs = [500, 1000, 2000, 5000, 10000];

export function realtimeURL(protocol: string, host: string, ticket: string): string {
  const socketProtocol = protocol === 'https:' ? 'wss:' : 'ws:';
  return `${socketProtocol}//${host}/api/ws?ticket=${encodeURIComponent(ticket)}`;
}

export async function connectRealtime(
  roomId: string,
  onEvent: (event: RealtimeEvent) => void,
  onStatus: (status: 'connecting' | 'connected' | 'disconnected') => void,
  options: RealtimeOptions = {}
): Promise<RealtimeConnection> {
  const createTicket = options.createTicket ?? createWSTicket;
  const WebSocketClass = options.WebSocketClass ?? WebSocket;
  const reconnectDelaysMs = options.reconnectDelaysMs ?? defaultReconnectDelaysMs;
  let socket: WebSocketLike | null = null;
  let closedByCaller = false;
  let reconnectAttempt = 0;
  let reconnectTimer: number | null = null;

  async function openSocket(statusOnStart: 'connecting' | null) {
    if (closedByCaller) return;
    if (statusOnStart) {
      onStatus(statusOnStart);
    }
    const ticket = await createTicket(roomId);
    if (closedByCaller) return;
    const nextSocket = new WebSocketClass(realtimeURL(window.location.protocol, window.location.host, ticket.ticket));
    socket = nextSocket;

    nextSocket.onopen = () => {
      reconnectAttempt = 0;
      onStatus('connected');
    };
    nextSocket.onclose = () => {
      if (closedByCaller || socket !== nextSocket) return;
      onStatus('disconnected');
      scheduleReconnect();
    };
    nextSocket.onerror = () => {
      if (!closedByCaller) {
        onStatus('disconnected');
      }
    };
    nextSocket.onmessage = (message) => {
      const event = JSON.parse(message.data) as RealtimeEvent;
      if (event.type === 'room.snapshot' && event.payload) {
        event.payload = normalizeRoomSnapshot(event.payload);
      }
      onEvent(event);
    };
  }

  function scheduleReconnect() {
    const delay = reconnectDelaysMs[Math.min(reconnectAttempt, reconnectDelaysMs.length - 1)];
    reconnectAttempt += 1;
    reconnectTimer = window.setTimeout(() => {
      reconnectTimer = null;
      void openSocket('connecting').catch(() => {
        if (!closedByCaller) {
          onStatus('disconnected');
          scheduleReconnect();
        }
      });
    }, delay);
  }

  await openSocket(null);

  return {
    send(command) {
      if (!socket || socket.readyState !== WebSocket.OPEN) return false;
      const requestId = crypto.randomUUID();
      socket.send(JSON.stringify({ ...command, requestId }));
      return true;
    },
    close() {
      closedByCaller = true;
      if (reconnectTimer !== null) {
        window.clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }
      socket?.close();
    }
  };
}
