import { describe, expect, it } from 'vitest';
import { hiddenUploaderDisplayName, trackUploaderName } from './audioCache';

describe('audio cache display helpers', () => {
  it('hides intentionally unknown restored uploaders', () => {
    expect(trackUploaderName({
      uploaderDisplayName: hiddenUploaderDisplayName,
      uploadedByName: 'Ada',
      uploadedByUserId: 'mod-one'
    } as never)).toBe('');
  });
});
