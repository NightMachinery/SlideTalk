import { describe, expect, it, vi } from 'vitest';
import { connectRealtime, realtimeURL } from './realtime';

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

    connection.send({ type: 'turn.next' });
    expect(FakeWebSocket.sockets[0].sent).toHaveLength(0);

    FakeWebSocket.sockets[0].open();
    connection.send({ type: 'turn.next' });
    expect(FakeWebSocket.sockets[0].sent).toHaveLength(1);
  });
});
