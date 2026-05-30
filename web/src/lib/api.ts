import type { RoomSnapshot } from './realtime';

export type User = {
  id: string;
  displayName: string;
  isAdmin: boolean;
  config?: {
    audioDriftThresholdSeconds?: number;
  };
};

export type Room = {
  id: string;
  title: string;
  hasPassword: boolean;
  expiresAt?: string | null;
  neverExpires?: boolean;
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

export type WSTicket = {
  ticket: string;
  expiresAt: string;
};

export type MigrationLink = {
  roomId: string;
  migrationId: string;
  expiresAt: string;
};

export type SlideStatus = {
  exists: boolean;
  sha256: string;
  alreadyUploaded: boolean;
  missing: boolean;
};

export type Admin = {
  id: string;
  displayName: string;
  createdAt: string;
};

export type RoomSettingsInput = {
  title?: string;
  password?: string;
  passwordAction?: 'set' | 'clear';
  roomMode?: 'slides' | 'markdown' | 'audio';
  allowParticipantMarkdown?: boolean;
  sharedNavigationEnabled?: boolean;
  allowAudienceAudioUpload?: boolean;
  allowAudienceAudioControl?: boolean;
  raiseHandMode?: 'off' | 'manual' | 'queue';
  showAudioStarCounts?: boolean;
  expiresAt?: string;
  neverExpires?: boolean;
};

const tokenKey = 'slidetalk.authToken';
export const insufficientFreeSpaceMessage = 'The server does not have enough free space. All uploads have been disabled. Contact the server admin to increase storage.';

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

export async function listAdmins(): Promise<Admin[]> {
  return api('/api/admins');
}

export async function demoteAdmin(userId: string): Promise<void> {
  return apiVoid(`/api/admins/${encodeURIComponent(userId)}`, { method: 'DELETE' });
}

export async function demoteAllAdmins(includeSelf: boolean): Promise<void> {
  return apiVoid('/api/admins/demote-all', {
    method: 'POST',
    body: JSON.stringify({ includeSelf })
  });
}

export async function createRoom(title: string, password: string, roomMode: 'slides' | 'markdown' | 'audio' = 'slides'): Promise<RoomDetails> {
  return api('/api/rooms', {
    method: 'POST',
    body: JSON.stringify({ title, password, roomMode })
  });
}

export async function joinRoom(roomId: string, password: string, migrationId = ''): Promise<RoomDetails> {
  return api(`/api/rooms/${encodeURIComponent(roomId)}/join`, {
    method: 'POST',
    body: JSON.stringify({ password, migrationId })
  });
}

export async function getRoom(roomId: string): Promise<RoomDetails> {
  return api(`/api/rooms/${encodeURIComponent(roomId)}`);
}

export async function getRoomSnapshot(roomId: string): Promise<RoomSnapshot> {
  return api(`/api/rooms/${encodeURIComponent(roomId)}/snapshot`);
}

export async function createWSTicket(roomId: string): Promise<WSTicket> {
  return api(`/api/rooms/${encodeURIComponent(roomId)}/ws-ticket`, {
    method: 'POST',
    body: JSON.stringify({})
  });
}

export async function createMigrationLink(roomId: string): Promise<MigrationLink> {
  return api(`/api/rooms/${encodeURIComponent(roomId)}/migration-link`, {
    method: 'POST',
    body: JSON.stringify({})
  });
}

export async function checkUploadPreflight(sizeBytes: number): Promise<void> {
  await api('/api/uploads/preflight', {
    method: 'POST',
    body: JSON.stringify({ sizeBytes })
  });
}

export async function updateRoomSettings(roomId: string, input: RoomSettingsInput): Promise<Room> {
  return api(`/api/rooms/${encodeURIComponent(roomId)}/settings`, {
    method: 'PATCH',
    body: JSON.stringify(input)
  });
}

export function slideFileRequest(roomId: string): { url: string; headers: Record<string, string> } {
  return {
    url: `/api/rooms/${encodeURIComponent(roomId)}/slide/file`,
    headers: { Authorization: `Bearer ${getAuthToken()}` }
  };
}

export async function getSlideStatus(sha256: string): Promise<SlideStatus> {
  return api(`/api/slides/${encodeURIComponent(sha256)}`);
}

export function uploadSlide(
  input: {
    roomId: string;
    sha256: string;
    expiresAt: string;
    originalName: string;
    file?: File;
  },
  onProgress: (percent: number) => void
): Promise<SlideStatus> {
  return uploadSlideRequest('/api/slides', input, onProgress);
}

export function uploadRoomSlide(
  input: {
    roomId: string;
    sha256: string;
    expiresAt: string;
    originalName: string;
    file?: File;
  },
  onProgress: (percent: number) => void
): Promise<SlideStatus> {
  return uploadSlideRequest(`/api/rooms/${encodeURIComponent(input.roomId)}/slide`, input, onProgress);
}

export async function updateRoomSlideExpiration(roomId: string, expiresAt: string): Promise<void> {
  return apiVoid(`/api/rooms/${encodeURIComponent(roomId)}/slide`, {
    method: 'PATCH',
    body: JSON.stringify({ expiresAt })
  });
}

export async function removeRoomSlide(roomId: string): Promise<void> {
  return apiVoid(`/api/rooms/${encodeURIComponent(roomId)}/slide`, { method: 'DELETE' });
}

export function audioFileRequest(roomId: string, trackId: string): { url: string; headers: Record<string, string> } {
  return {
    url: `/api/rooms/${encodeURIComponent(roomId)}/audio/${encodeURIComponent(trackId)}`,
    headers: { Authorization: `Bearer ${getAuthToken()}` }
  };
}

export function audioCoverRequest(roomId: string, trackId: string): { url: string; headers: Record<string, string> } {
  return {
    url: `/api/rooms/${encodeURIComponent(roomId)}/audio/${encodeURIComponent(trackId)}/cover`,
    headers: { Authorization: `Bearer ${getAuthToken()}` }
  };
}

export async function createAudioDownloadLink(roomId: string, trackId: string): Promise<{ url: string }> {
  return api(`/api/rooms/${encodeURIComponent(roomId)}/audio/${encodeURIComponent(trackId)}/download-link`, {
    method: 'POST',
    body: JSON.stringify({})
  });
}

export async function updateRoomAudio(
  roomId: string,
  trackId: string,
  input: { title?: string; uploaderDisplayName?: string }
): Promise<void> {
  return apiVoid(`/api/rooms/${encodeURIComponent(roomId)}/audio/${encodeURIComponent(trackId)}`, {
    method: 'PATCH',
    body: JSON.stringify(input)
  });
}

export function uploadRoomAudio(
  input: {
    roomId: string;
    sha256: string;
    originalName: string;
    file: File;
    metadataTitle?: string;
    durationSeconds?: number;
    cover?: Blob;
    coverMIMEType?: string;
  },
  onProgress: (percent: number) => void
): Promise<{
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
  alreadyUploaded: boolean;
  missing: boolean;
}> {
  const form = new FormData();
  form.set('sha256', input.sha256);
  form.set('originalName', input.originalName);
  if (input.metadataTitle) form.set('metadataTitle', input.metadataTitle);
  if (input.durationSeconds && input.durationSeconds > 0) form.set('durationSeconds', String(Math.round(input.durationSeconds)));
  if (input.cover) form.set('cover', input.cover, 'cover');
  form.set('file', input.file, input.originalName);

  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    request.open('POST', `/api/rooms/${encodeURIComponent(input.roomId)}/audio`);
    request.setRequestHeader('Authorization', `Bearer ${getAuthToken()}`);
    request.upload.onprogress = (event) => {
      if (event.lengthComputable) {
        onProgress(Math.round((event.loaded / event.total) * 100));
      }
    };
    request.onload = () => {
      if (request.status < 200 || request.status >= 300) {
        try {
          const problem = JSON.parse(request.responseText) as { detail?: string };
          reject(new Error(problem.detail ?? 'Request failed.'));
        } catch {
          reject(new Error('Request failed.'));
        }
        return;
      }
      resolve(JSON.parse(request.responseText));
    };
    request.onerror = () => reject(new Error('Audio upload failed.'));
    request.send(form);
  });
}

export async function removeRoomAudio(roomId: string, trackId: string): Promise<void> {
  return apiVoid(`/api/rooms/${encodeURIComponent(roomId)}/audio/${encodeURIComponent(trackId)}`, { method: 'DELETE' });
}

function uploadSlideRequest(
  path: string,
  input: {
    roomId: string;
    sha256: string;
    expiresAt: string;
    originalName: string;
    file?: File;
  },
  onProgress: (percent: number) => void
): Promise<SlideStatus> {
  const form = new FormData();
  form.set('roomId', input.roomId);
  form.set('sha256', input.sha256);
  form.set('expiresAt', input.expiresAt);
  form.set('originalName', input.originalName);
  if (input.file) {
    form.set('file', input.file, input.originalName);
  }

  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    request.open('POST', path);
    request.setRequestHeader('Authorization', `Bearer ${getAuthToken()}`);
    request.upload.onprogress = (event) => {
      if (event.lengthComputable) {
        onProgress(Math.round((event.loaded / event.total) * 100));
      }
    };
    request.onload = () => {
      if (request.status < 200 || request.status >= 300) {
        try {
          const problem = JSON.parse(request.responseText) as { detail?: string };
          reject(new Error(problem.detail ?? 'Request failed.'));
        } catch {
          reject(new Error('Request failed.'));
        }
        return;
      }
      resolve(JSON.parse(request.responseText) as SlideStatus);
    };
    request.onerror = () => reject(new Error('Slide upload failed.'));
    request.send(form);
  });
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

async function apiVoid(path: string, init: RequestInit = {}): Promise<void> {
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
}
