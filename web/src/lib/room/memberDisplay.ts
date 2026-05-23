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

export function displayNameForRoom(member: SnapshotMember, members: SnapshotMember[]) {
  const cleanName = member.displayName.trim();
  const sameName = members
    .filter((item) => item.displayName.trim().toLocaleLowerCase() === cleanName.toLocaleLowerCase())
    .sort((left, right) => left.displayOrder - right.displayOrder || left.userId.localeCompare(right.userId));
  if (sameName.length <= 1) return member.displayName;
  const index = sameName.findIndex((item) => item.userId === member.userId);
  return `${member.displayName} ${index + 1}`;
}
