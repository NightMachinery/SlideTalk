import { describe, expect, it } from 'vitest';
import { classifyAudioUploadFiles, isSupportedAudioUpload, safeBrowserAudio } from './audioUploadValidation';

function file(name: string, type: string, bytes: Uint8Array) {
  return new File([bytes], name, { type });
}

const m4aBytes = new Uint8Array([0, 0, 0, 24, ...new TextEncoder().encode('ftypM4A \0\0\0\0M4A mp42isom')]);

describe('audio upload validation', () => {
  it('accepts m4a files across common browser MIME labels', async () => {
    await expect(isSupportedAudioUpload(file('song.m4a', 'audio/x-m4a', m4aBytes))).resolves.toBe(true);
    await expect(isSupportedAudioUpload(file('song.m4a', 'audio/m4a', m4aBytes))).resolves.toBe(true);
    await expect(isSupportedAudioUpload(file('song.m4a', 'audio/mp4', m4aBytes))).resolves.toBe(true);
    await expect(isSupportedAudioUpload(file('song.m4a', 'application/mp4', m4aBytes))).resolves.toBe(true);
    await expect(isSupportedAudioUpload(file('song.m4a', '', m4aBytes))).resolves.toBe(true);
    await expect(isSupportedAudioUpload(file('song.m4a', 'application/octet-stream', m4aBytes))).resolves.toBe(true);
  });

  it('rejects unsupported files before upload', async () => {
    await expect(isSupportedAudioUpload(file('notes.txt', 'text/plain', new TextEncoder().encode('not audio')))).resolves.toBe(false);
    await expect(isSupportedAudioUpload(file('fake.m4a', 'application/octet-stream', new TextEncoder().encode('not audio')))).resolves.toBe(false);
  });

  it('rejects video mp4 unless the file extension is audio-specific', async () => {
    await expect(isSupportedAudioUpload(file('clip.mp4', 'video/mp4', m4aBytes))).resolves.toBe(false);
    await expect(isSupportedAudioUpload(file('song.m4a', 'video/mp4', m4aBytes))).resolves.toBe(true);
  });

  it('splits mixed selections into supported and skipped files', async () => {
    const song = file('song.m4a', 'application/octet-stream', m4aBytes);
    const notes = file('notes.txt', 'text/plain', new TextEncoder().encode('not audio'));

    await expect(classifyAudioUploadFiles([song, notes])).resolves.toEqual({
      supported: [song],
      unsupported: [notes]
    });
  });

  it('treats extension-supported audio as browser-safe when MIME is missing', () => {
    expect(safeBrowserAudio(file('song.m4a', '', m4aBytes))).toBe(true);
  });
});
