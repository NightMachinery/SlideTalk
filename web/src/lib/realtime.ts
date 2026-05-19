import { createWSTicket } from './api';

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
    noSlideMode: boolean;
    allowParticipantMarkdown: boolean;
  };
  caller: {
    userId: string;
    role: 'mod' | 'participant' | 'observer';
    isAdmin: boolean;
  };
  participants: SnapshotMember[];
  observers: SnapshotMember[];
  currentTurn: unknown;
  timer: unknown;
  hands: unknown[];
  slide: unknown;
  markdown: string;
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
    };

export type RealtimeConnection = {
  send(command: RealtimeCommand): void;
  close(): void;
};

export async function connectRealtime(
  roomId: string,
  onEvent: (event: RealtimeEvent) => void,
  onStatus: (status: 'connected' | 'disconnected') => void
): Promise<RealtimeConnection> {
  const ticket = await createWSTicket(roomId);
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const socket = new WebSocket(`${protocol}//${window.location.host}/api/ws?ticket=${encodeURIComponent(ticket.ticket)}`);

  socket.onopen = () => onStatus('connected');
  socket.onclose = () => onStatus('disconnected');
  socket.onerror = () => onStatus('disconnected');
  socket.onmessage = (message) => {
    onEvent(JSON.parse(message.data) as RealtimeEvent);
  };

  return {
    send(command) {
      const requestId = crypto.randomUUID();
      socket.send(JSON.stringify({ ...command, requestId }));
    },
    close() {
      socket.close();
    }
  };
}
