export const DEFAULT_AUDIO_TAGS = [
  'Instrumental',
  'Persian',
  'Calm',
  'Energetic',
  'Ambient',
  'Dramatic',
  'Upbeat',
  'Background'
] as const;

export function audioTagSlug(label: string) {
  return label
    .trim()
    .toLocaleLowerCase()
    .replace(/[^\p{Letter}\p{Number}]+/gu, '-')
    .replace(/^-+|-+$/g, '');
}
