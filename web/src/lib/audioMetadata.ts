import { parseBlob } from 'music-metadata';

export type AudioUploadMetadata = {
  metadataTitle: string;
  durationSeconds: number;
  cover?: Blob;
};

export async function readAudioUploadMetadata(file: File): Promise<AudioUploadMetadata> {
  try {
    const metadata = await parseBlob(file, { duration: true, skipCovers: false });
    const picture = metadata.common.picture?.[0];
    return {
      metadataTitle: metadata.common.title?.trim() ?? '',
      durationSeconds: Number.isFinite(metadata.format.duration) ? Math.max(0, Math.round(metadata.format.duration ?? 0)) : 0,
      cover: picture ? new Blob([picture.data], { type: picture.format }) : undefined
    };
  } catch {
    return { metadataTitle: '', durationSeconds: 0 };
  }
}
