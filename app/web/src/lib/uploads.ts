import type { AttachedFile, AttachedImage } from './chatStore';
import type { PrivatemodeClient } from './clientStore';

export const SUPPORTED_IMAGE_TYPES = [
  'image/png',
  'image/jpeg',
  'image/gif',
  'image/webp',
  'image/avif',
];

export const SUPPORTED_AUDIO_TYPES = [
  'audio/aac',
  'audio/flac',
  'audio/m4a',
  'audio/mp3',
  'audio/mpeg',
  'audio/mp4',
  'audio/ogg',
  'audio/wav',
  'audio/webm',
  'audio/x-m4a',
];

export const SUPPORTED_AUDIO_EXTENSIONS = [
  '.aac',
  '.flac',
  '.m4a',
  '.mp3',
  '.mp4',
  '.mpeg',
  '.mpga',
  '.oga',
  '.ogg',
  '.wav',
  '.webm',
];

export const MAX_AUDIO_FILE_SIZE_BYTES = 50 * 1024 * 1024;

export function isSupportedImage(type: string): boolean {
  return SUPPORTED_IMAGE_TYPES.includes(type);
}

export function isSupportedAudio(file: globalThis.File): boolean {
  const normalizedName = file.name.toLowerCase();
  return (
    SUPPORTED_AUDIO_TYPES.includes(file.type) ||
    SUPPORTED_AUDIO_EXTENSIONS.some((ext) => normalizedName.endsWith(ext))
  );
}

export interface ClassifiedFiles {
  images: globalThis.File[];
  audio: globalThis.File[];
  documents: globalThis.File[];
  unsupportedImages: globalThis.File[];
}

export function classifyFiles(
  files: Iterable<globalThis.File>,
  supportsVision: boolean,
): ClassifiedFiles {
  const images: globalThis.File[] = [];
  const audio: globalThis.File[] = [];
  const documents: globalThis.File[] = [];
  const unsupportedImages: globalThis.File[] = [];
  for (const f of files) {
    if (isSupportedImage(f.type)) {
      if (supportsVision) images.push(f);
      else unsupportedImages.push(f);
    } else if (isSupportedAudio(f)) {
      audio.push(f);
    } else {
      documents.push(f);
    }
  }
  return { images, audio, documents, unsupportedImages };
}

export function readImageAsAttached(
  file: globalThis.File,
  fallbackName?: string,
): Promise<AttachedImage> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () =>
      resolve({
        id: crypto.randomUUID(),
        name: file.name || fallbackName || 'pasted-image.png',
        dataUrl: reader.result as string,
      });
    reader.onerror = () =>
      reject(reader.error ?? new Error('Failed to read image'));
    reader.readAsDataURL(file);
  });
}

export async function extractDocumentText(
  client: PrivatemodeClient,
  file: globalThis.File,
): Promise<AttachedFile> {
  const content = new Uint8Array(await file.arrayBuffer());
  const elements = (await client.unstructured(
    [{ name: file.name, content, contentType: file.type }],
    { strategy: 'fast' },
  )) as Array<{ text: string }>;
  return {
    name: file.name,
    content: elements.map((el) => el.text).join('\n\n'),
  };
}

export async function transcribeAudio(
  client: PrivatemodeClient,
  file: globalThis.File,
  model: string,
): Promise<AttachedFile> {
  if (file.size > MAX_AUDIO_FILE_SIZE_BYTES) {
    throw new Error(`${file.name} exceeds the 50 MB audio upload limit`);
  }
  const transcription = await client.audio.transcriptions.create({
    file,
    model,
  });

  const text = transcription.text?.trim();
  if (!text) {
    throw new Error(`${file.name} did not produce a transcription`);
  }

  return {
    name: file.name,
    content: text,
    kind: 'audio-transcription',
  };
}
