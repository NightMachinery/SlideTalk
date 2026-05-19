export type User = {
  id: string;
  displayName: string;
  isAdmin: boolean;
};

export type Room = {
  id: string;
  title: string;
  hasPassword: boolean;
};

export type Membership = {
  roomId: string;
  userId: string;
  role: 'mod' | 'participant' | 'observer';
  displayOrder: number;
};

export type RoomDetails = {
  room: Room;
  membership: Membership;
};

const tokenKey = 'slidetalk.authToken';

export function getAuthToken(): string {
  const existing = localStorage.getItem(tokenKey);
  if (existing) return existing;

  const bytes = new Uint8Array(32);
  crypto.getRandomValues(bytes);
  const token = btoa(String.fromCharCode(...bytes)).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '');
  localStorage.setItem(tokenKey, token);
  return token;
}

export async function getMe(): Promise<User> {
  return api('/api/me');
}

export async function updateProfile(displayName: string): Promise<User> {
  return api('/api/me', {
    method: 'PATCH',
    body: JSON.stringify({ displayName })
  });
}

export async function submitAdminToken(token: string): Promise<User> {
  return api('/api/me/admin-token', {
    method: 'POST',
    body: JSON.stringify({ token })
  });
}

export async function createRoom(title: string, password: string): Promise<RoomDetails> {
  return api('/api/rooms', {
    method: 'POST',
    body: JSON.stringify({ title, password })
  });
}

export async function joinRoom(roomId: string, password: string): Promise<RoomDetails> {
  return api(`/api/rooms/${encodeURIComponent(roomId)}/join`, {
    method: 'POST',
    body: JSON.stringify({ password })
  });
}

export async function getRoom(roomId: string): Promise<RoomDetails> {
  return api(`/api/rooms/${encodeURIComponent(roomId)}`);
}

async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${getAuthToken()}`,
      ...init.headers
    }
  });

  if (!response.ok) {
    const problem = await response.json().catch(() => ({ detail: 'Request failed.' }));
    throw new Error(problem.detail ?? 'Request failed.');
  }

  return response.json() as Promise<T>;
}

