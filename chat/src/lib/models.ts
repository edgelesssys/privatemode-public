import anonymousPolicy from '../../../internal/anonymous/policy.json' with { type: 'json' };
import type { Locale } from './i18n';

export const DEFAULT_MODEL_ID = 'glm-flash-latest';
export const ANONYMOUS_MAX_CHARACTERS = anonymousPolicy.maxCharacters;

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

function getSystemPrompt(
  model: keyof typeof anonymousPolicy.models,
): Record<Locale, string> {
  const modelName = anonymousPolicy.models[model];
  return {
    en: anonymousPolicy.systemPrompts.en.replace('{model}', modelName),
    de: anonymousPolicy.systemPrompts.de.replace('{model}', modelName),
  };
}

export const modelConfig: Record<string, ModelInfo> = {
  'glm-flash-latest': {
    displayName: 'GLM-5.3-Flash',
    subtitle:
      'A fast model that combines strong all-round capabilities with vision',
    systemPrompt: getSystemPrompt('glm-flash-latest'),
    thinkingMode: 'extended',
    reasoningEffort: { normal: 'low', extended: 'max' },
    supportsFileUploads: true,
    supportsVision: true,
    maxWords: 70000,
  },
  'glm-latest': {
    displayName: 'GLM-5.3',
    subtitle: 'Our largest model for your most ambitious tasks',
    systemPrompt: getSystemPrompt('glm-latest'),
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
    systemPrompt: getSystemPrompt('openai/gpt-oss-120b'),
    thinkingMode: 'extended',
    reasoningEffort: { normal: 'low', extended: 'high' },
    supportsFileUploads: true,
    supportsVision: false,
    maxWords: 70000,
  },
};
