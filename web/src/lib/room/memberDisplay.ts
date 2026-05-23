import type { SnapshotMember } from '../realtime';

export function sortedByOnline(members: SnapshotMember[]) {
  return [...members].sort((left, right) => Number(right.isOnline) - Number(left.isOnline));
}

export function onlineCount(members: SnapshotMember[]) {
  return members.filter((member) => member.isOnline).length;
}

export function onlineCountLabel(members: SnapshotMember[]) {
  return `${onlineCount(members)}/${members.length}`;
}
