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
    noSlideMode: boolean;
    allowParticipantMarkdown: boolean;
    raiseHandMode: 'off' | 'manual' | 'queue';
    slidePage: number;
    sharedNavigationEnabled: boolean;
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
    expiresAt: string;
    missing: boolean;
  } | null;
  markdown: string;
  markdownUpdatedByUserId: string;
  markdownUpdatedByName: string;
  markdownUpdatedAt: string;
};

export type RealtimeEvent = {
  type: 'room.snapshot' | 'error';
  requestId?: string;
  roomId?: string;
  version?: number;
  code?: string;
  message?: string;
  payload?: RoomSnapshot;
};

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
        noSlideMode?: boolean;
        allowParticipantMarkdown?: boolean;
      };
    }
  | {
      type: 'slide.navigate';
      payload: { page: number; modSharedNavigationEnabled: boolean };
    }
  | {
      type: 'markdown.update';
      payload: { markdown: string };
    };

export type RealtimeConnection = {
  send(command: RealtimeCommand): void;
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
      onEvent(JSON.parse(message.data) as RealtimeEvent);
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
      if (!socket || socket.readyState !== WebSocket.OPEN) return;
      const requestId = crypto.randomUUID();
      socket.send(JSON.stringify({ ...command, requestId }));
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
