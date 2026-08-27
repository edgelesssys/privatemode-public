import { writable } from 'svelte/store';
import type { Model } from './clientStore';

export const DEFAULT_TRANSCRIPTION_MODEL_ID = 'whisper-large-v3';
export const TRANSCRIPTION_TASK = 'transcribe';

const STORAGE_KEY = 'privatemode_transcription_model';

export interface TranscriptionModelInfo {
  displayName: string;
  description: string;
}

export const transcriptionModelConfig = {
  'whisper-large-v3': {
    displayName: 'Whisper Large v3',
    description: 'OpenAI',
  },
  'voxtral-mini-3b': {
    displayName: 'Voxtral Mini 3B',
    description: 'Mistral',
  },
} satisfies Record<string, TranscriptionModelInfo>;

export type TranscriptionModelID = keyof typeof transcriptionModelConfig;
export type AvailableTranscriptionModel = Model & {
  id: TranscriptionModelID;
};

function isSupportedTranscriptionModelID(
  value: string,
): value is TranscriptionModelID {
  return value in transcriptionModelConfig;
}

export function getAvailableTranscriptionModels(
  models: Model[],
): AvailableTranscriptionModel[] {
  return Object.keys(transcriptionModelConfig)
    .map((id) =>
      models.find(
        (model) => model.id === id && model.tasks?.includes(TRANSCRIPTION_TASK),
      ),
    )
    .filter(
      (model): model is AvailableTranscriptionModel => model !== undefined,
    );
}

function getStoredTranscriptionModel(): TranscriptionModelID {
  if (typeof localStorage === 'undefined')
    return DEFAULT_TRANSCRIPTION_MODEL_ID;
  const stored = localStorage.getItem(STORAGE_KEY);
  return stored && isSupportedTranscriptionModelID(stored)
    ? stored
    : DEFAULT_TRANSCRIPTION_MODEL_ID;
}

function createTranscriptionModelStore() {
  const store = writable<TranscriptionModelID>(getStoredTranscriptionModel());

  return {
    subscribe: store.subscribe,
    set(value: TranscriptionModelID) {
      store.set(value);
      if (typeof localStorage !== 'undefined') {
        localStorage.setItem(STORAGE_KEY, value);
      }
    },
  };
}

export const transcriptionModel = createTranscriptionModelStore();
