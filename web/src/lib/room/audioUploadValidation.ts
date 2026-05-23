const octetMime = 'application/octet-stream';
const audioExtensionMimes: Record<string, string> = {
  mp3: 'audio/mpeg',
  m4a: 'audio/mp4',
  m4b: 'audio/mp4',
  aac: 'audio/aac',
  ogg: 'audio/ogg',
  oga: 'audio/ogg',
  opus: 'audio/opus',
  wav: 'audio/wav',
  flac: 'audio/flac',
  webm: 'audio/webm',
  weba: 'audio/webm'
};

export type AudioUploadClassification = {
  supported: File[];
  unsupported: File[];
};

export async function classifyAudioUploadFiles(files: Iterable<File>): Promise<AudioUploadClassification> {
  const supported: File[] = [];
  const unsupported: File[] = [];
  for (const file of files) {
    if (await isSupportedAudioUpload(file)) {
      supported.push(file);
    } else {
      unsupported.push(file);
    }
  }
  return { supported, unsupported };
}

export async function isSupportedAudioUpload(file: File): Promise<boolean> {
  const mime = normalizeMime(file.type);
  const ext = extensionFromName(file.name);
  if (mp4AudioExtension(ext) && (mime === '' || mime === octetMime || mime === 'video/mp4' || mime === 'application/mp4' || mime.startsWith('audio/'))) {
    return await hasMP4FileType(file);
  }
  if (mime === '' || mime === octetMime) {
    return Boolean(audioExtensionMimes[ext]);
  }
  if (mime === 'application/ogg') {
    return Boolean(audioExtensionMimes[ext]);
  }
  if (mime.startsWith('audio/')) {
    return true;
  }
  return false;
}

export function safeBrowserAudio(file: File) {
  const type = normalizeMime(file.type);
  const ext = extensionFromName(file.name);
  return Boolean(audioExtensionMimes[ext]) || ['audio/mpeg', 'audio/mp4', 'audio/m4a', 'audio/aac', 'audio/ogg', 'audio/opus', 'audio/wav', 'audio/flac', 'audio/webm', 'audio/x-m4a'].includes(type);
}

function normalizeMime(type: string) {
  return type.toLowerCase().split(';')[0].trim();
}

function extensionFromName(name: string) {
  const index = name.trim().toLowerCase().lastIndexOf('.');
  return index >= 0 ? name.trim().toLowerCase().slice(index + 1) : '';
}

function mp4AudioExtension(ext: string) {
  return ext === 'm4a' || ext === 'm4b';
}

async function hasMP4FileType(file: File) {
  const prefix = new Uint8Array(await blobArrayBuffer(file.slice(0, 32)));
  if (prefix.length < 8) return false;
  const marker = String.fromCharCode(...prefix.slice(4, 8));
  return marker === 'ftyp';
}

function blobArrayBuffer(blob: Blob): Promise<ArrayBuffer> {
  if ('arrayBuffer' in blob && typeof blob.arrayBuffer === 'function') {
    return blob.arrayBuffer();
  }
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result as ArrayBuffer);
    reader.onerror = () => reject(reader.error ?? new Error('Could not read file.'));
    reader.readAsArrayBuffer(blob);
  });
}
