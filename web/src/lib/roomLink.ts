export function roomIdFromInput(input: string, base = 'http://localhost/'): string {
  const trimmed = input.trim();
  if (trimmed === '') return '';
  if (!looksLikeURL(trimmed)) return trimmed;

  try {
    const url = new URL(trimmed, base);
    return url.searchParams.get('room')?.trim() || trimmed;
  } catch {
    return trimmed;
  }
}

function looksLikeURL(value: string): boolean {
  return value.startsWith('http://') || value.startsWith('https://') || value.startsWith('/') || value.startsWith('?');
}
