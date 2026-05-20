export function pageFromSharedNavigation(input: {
  localPage: number;
  sharedPage: number;
  followShared: boolean;
}): number {
  if (!input.followShared) return input.localPage;
  return Math.max(1, Math.round(input.sharedPage));
}
