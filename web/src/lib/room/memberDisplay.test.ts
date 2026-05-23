import { describe, expect, it } from 'vitest';
import { displayNameForRoom, onlineCount, onlineCountLabel, sortedByOnline } from './memberDisplay';
import type { SnapshotMember } from '../realtime';

function member(userId: string, displayOrder: number, isOnline: boolean): SnapshotMember {
  return {
    userId,
    displayName: userId,
    role: 'participant',
    displayOrder,
    isOnline
  };
}

describe('member display helpers', () => {
  it('sorts online members before offline members while preserving relative order', () => {
    const offlineFirst = member('offline-first', 1, false);
    const onlineFirst = member('online-first', 2, true);
    const offlineSecond = member('offline-second', 3, false);
    const onlineSecond = member('online-second', 4, true);

    expect(sortedByOnline([offlineFirst, onlineFirst, offlineSecond, onlineSecond])).toEqual([
      onlineFirst,
      onlineSecond,
      offlineFirst,
      offlineSecond
    ]);
  });

  it('formats online count labels as online over total', () => {
    const members = [member('offline', 1, false), member('online-one', 2, true), member('online-two', 3, true)];

    expect(onlineCount(members)).toBe(2);
    expect(onlineCountLabel(members)).toBe('2/3');
  });

  it('adds stable suffixes only to duplicate display names', () => {
    const first = { ...member('first', 2, false), displayName: 'Ada' };
    const second = { ...member('second', 1, true), displayName: 'Grace' };
    const third = { ...member('third', 0, true), displayName: 'Ada' };
    const members = [first, second, third];

    expect(displayNameForRoom(first, members)).toBe('Ada 2');
    expect(displayNameForRoom(second, members)).toBe('Grace');
    expect(displayNameForRoom(third, members)).toBe('Ada 1');
  });
});
