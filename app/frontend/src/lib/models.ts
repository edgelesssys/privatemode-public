export const DEFAULT_MODEL_ID = 'openai/gpt-oss-120b';

export interface ModelInfo {
  displayName: string;
  subtitle: string;
  systemPrompt: string;
  supportsExtendedThinking?: boolean;
  supportsFileUploads: boolean;
  maxWords: number;
}

function getSystemPrompt(modelName: string): string {
  return `
    You, ${modelName}, run as part of the AI service Privatemode AI, which is developed by Edgeless Systems.
    You run inside a secure environment based on confidential computing (AMD SEV-SNP, with NVIDIA H100 GPUs).
    The environment cannot be accessed from the outside and user data remains encrypted in memory during processing.
    All the data you process is end-to-end encrypted, and even Edgeless Systems or the cloud provider cannot access the data.
    Because of these security guarantees, you can perfectly handle prompts and file uploads with sensitive information
    such as tax returns, doctor's notes, or other personal data.
    If the user has problems with Privatemode, refer him to https://www.privatemode.ai/contact for support.
    You are a helpful assistant answering user questions concisely and to the point.
    You don't talk about yourself unless asked.
    `.trim();
}

export const modelConfig: Record<string, ModelInfo> = {
  'openai/gpt-oss-120b': {
    displayName: 'gpt-oss-120b',
    subtitle: 'Reasoning model suited for complex tasks',
    systemPrompt: getSystemPrompt('gpt-oss-120b'),
    supportsExtendedThinking: true,
    supportsFileUploads: true,
    maxWords: 70000,
  },
  'leon-se/gemma-3-27b-it-fp8-dynamic': {
    displayName: 'Gemma 3 27B',
    subtitle: 'Multi-modal model with image understanding',
    systemPrompt: getSystemPrompt('Gemma 3 27B'),
    supportsFileUploads: false,
    maxWords: 70000,
  },
  'qwen3-coder-30b-a3b': {
    displayName: 'Qwen3 Coder 30B',
    subtitle: 'Coding-specialized model for programming tasks',
    systemPrompt: getSystemPrompt('Qwen3 Coder 30B'),
    supportsFileUploads: true,
    maxWords: 70000,
  },
};
