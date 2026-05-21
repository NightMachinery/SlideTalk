import { describe, expect, it } from 'vitest';
import { roomIdFromInput } from './roomLink';

describe('roomIdFromInput', () => {
  it('keeps a raw room id', () => {
    expect(roomIdFromInput('room-123')).toBe('room-123');
  });

  it('extracts room id from an absolute room link', () => {
    expect(roomIdFromInput('https://talk.example/?room=room-123&migration=secret')).toBe('room-123');
  });

  it('extracts room id from a relative room link', () => {
    expect(roomIdFromInput('/?room=room-123')).toBe('room-123');
  });
});
