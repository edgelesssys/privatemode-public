import type { Locale } from './i18n';

export const DEFAULT_MODEL_ID = 'glm-flash-latest';

// Effort levels accepted by the models that expose `reasoning_effort`. Each model
// only honours a subset: gpt-oss takes low/medium/high, glm low/high/max.
export type ReasoningEffort = 'low' | 'medium' | 'high' | 'max';

export interface ModelInfo {
  displayName: string;
  subtitle: string;
  systemPrompt: Record<Locale, string>;
  thinkingMode?: 'extended' | 'toggle';
  thinkingToggleParam?: 'thinking' | 'enable_thinking';
  thinkingToggleAlwaysSend?: boolean;
  // For thinkingMode 'extended': effort sent with the button off and on. Off is the
  // cheapest level the model honours and on is its maximum, so the button spans the
  // same range as kimi's thinking toggle.
  reasoningEffort?: { normal: ReasoningEffort; extended: ReasoningEffort };
  supportsFileUploads: boolean;
  supportsVision: boolean;
  maxWords: number;
}

function getSystemPrompt(modelName: string): Record<Locale, string> {
  return {
    en: `
You are Privatemode Assistant and run as part of the AI service Privatemode AI, which is developed by Edgeless Systems.
You run inside a secure environment based on confidential computing (AMD SEV-SNP, with NVIDIA H100 GPUs).
The environment cannot be accessed from the outside and user data remains encrypted in memory during processing.
All the data you process is end-to-end encrypted, and even Edgeless Systems or the cloud provider cannot access the data.
Because of these security guarantees, you can perfectly handle prompts and file uploads with sensitive information
such as tax returns, doctor's notes, or other personal data.
If the user has problems with Privatemode, refer them to https://www.privatemode.ai/contact for support.
You are a helpful assistant answering user questions concisely and to the point.
You don't talk about yourself unless asked.
You are based on the LLM ${modelName}.
    `.trim(),
    de: `
Du bist der Privatemode-Assistent und wirst als Teil des KI-Dienstes Privatemode AI ausgeführt, der von Edgeless Systems entwickelt wird.
Du wirst in einer sicheren Umgebung ausgeführt, die auf Confidential Computing basiert (AMD SEV-SNP mit NVIDIA-H100-GPUs).
Auf die Umgebung kann von außen nicht zugegriffen werden, und Benutzerdaten bleiben während der Verarbeitung im Arbeitsspeicher verschlüsselt.
Alle Daten, die du verarbeitest, sind Ende-zu-Ende verschlüsselt. Selbst Edgeless Systems und der Cloud-Anbieter können nicht auf sie zugreifen.
Dank dieser Sicherheitsgarantien kannst du Anfragen und Datei-Uploads mit sensiblen Informationen wie Steuererklärungen, Arztbriefen oder anderen personenbezogenen Daten sicher verarbeiten.
Wenn der Benutzer Probleme mit Privatemode hat, verweise ihn für Support auf https://www.privatemode.ai/contact.
Du bist ein hilfreicher Assistent und beantwortest Benutzerfragen knapp und auf den Punkt.
Antworte standardmäßig auf Deutsch, aber folge einer vom Benutzer gewünschten Sprache.
Sprich nicht über dich selbst, außer du wirst danach gefragt.
Du basierst auf dem LLM ${modelName}.
    `.trim(),
  };
}

export const modelConfig: Record<string, ModelInfo> = {
  'glm-flash-latest': {
    displayName: 'GLM-5.3-Flash',
    subtitle:
      'A fast model that combines strong all-round capabilities with vision',
    systemPrompt: getSystemPrompt('GLM-5.3-Flash'),
    thinkingMode: 'extended',
    reasoningEffort: { normal: 'low', extended: 'max' },
    supportsFileUploads: true,
    supportsVision: true,
    maxWords: 70000,
  },
  'glm-latest': {
    displayName: 'GLM-5.3',
    subtitle: 'Our largest model for your most ambitious tasks',
    systemPrompt: getSystemPrompt('GLM-5.3'),
    // glm can't switch reasoning off: enable_thinking=false only empties
    // reasoning_content while the model keeps reasoning inline in the answer at
    // the same token cost. Offer the gpt-oss-style effort button instead.
    thinkingMode: 'extended',
    reasoningEffort: { normal: 'low', extended: 'max' },
    supportsFileUploads: true,
    supportsVision: false,
    maxWords: 70000,
  },
  'openai/gpt-oss-120b': {
    displayName: 'gpt-oss-120b',
    subtitle: 'A fast model from OpenAI with good text capabilities',
    systemPrompt: getSystemPrompt('gpt-oss-120b'),
    thinkingMode: 'extended',
    reasoningEffort: { normal: 'low', extended: 'high' },
    supportsFileUploads: true,
    supportsVision: false,
    maxWords: 70000,
  },
};
