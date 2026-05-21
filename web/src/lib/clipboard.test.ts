import { describe, expect, it, vi } from 'vitest';
import { copyText } from './clipboard';

describe('copyText', () => {
  it('reports copied when navigator clipboard succeeds', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);

    await expect(copyText('room-link', { clipboard: { writeText } })).resolves.toEqual({ copied: true });
    expect(writeText).toHaveBeenCalledWith('room-link');
  });

  it('returns fallback text when clipboard is unavailable or blocked', async () => {
    const writeText = vi.fn().mockRejectedValue(new Error('blocked'));

    await expect(copyText('room-link', { clipboard: { writeText } })).resolves.toEqual({
      copied: false,
      text: 'room-link'
    });
  });
});
