<script context="module" lang="ts">
  import { getChatDefaults, getDefaultModel, getExcludeFromProfile } from './Settings.svelte'
  import { get, writable } from 'svelte/store'
  import { getActiveModels } from './Models.svelte'
  // Profile definitions
  import { addMessage, clearMessages, deleteMessage, getChat, getChatSettings, getCustomProfiles, getGlobalSettings, getMessages, newName, resetChatSettings, saveChatStore, setGlobalSettingValueByKey, setMessages, updateProfile } from './Storage.svelte'
  import type { Message, SelectOption, ChatSettings } from './Types.svelte'
  import { v4 as uuidv4 } from 'uuid'

const defaultProfile = 'gptoss'

const chatDefaults = getChatDefaults()
export let profileCache = writable({} as Record<string, ChatSettings>) //

export const isStaticProfile = (key:string):boolean => {
    return !!profiles[key]
}

export const getProfiles = async (forceUpdate:boolean = false): Promise<Record<string, ChatSettings>> => {
    const defaultModel = await getDefaultModel()
    const availableModelIds = await getActiveModels(forceUpdate)

    const pc = get(profileCache)
    if (!forceUpdate && Object.keys(pc).length) {
      return pc
    }

    const result = Object.entries(profiles
    ).reduce((a, [k, v]) => {
      v = JSON.parse(JSON.stringify(v))
      // Only include profiles whose models are available in the API
      const modelId = v.modelConfig?.id || v.model || defaultModel
      if (Object.keys(availableModelIds).length === 0 || availableModelIds[modelId]) {
        a[k] = v
        v.model = v.model || defaultModel
      }
      return a
    }, {} as Record<string, ChatSettings>)
  
    Object.entries(getCustomProfiles()).forEach(([k, v]) => {
      updateProfile(v, true)
      // Also filter custom profiles based on available models
      const modelId = v.modelConfig?.id || v.model || defaultModel
      if (Object.keys(availableModelIds).length === 0 || availableModelIds[modelId]) {
        result[k] = v
      }
    })
  
    Object.entries(result).forEach(([k, v]) => {
      pc[k] = v
    })
    Object.keys(pc).forEach((k) => {
      if (!(k in result)) delete pc[k]
    })
    profileCache.set(pc)
    return result
}

// Return profiles list.
export const getProfileSelect = async ():Promise<SelectOption[]> => {
    const allProfiles = await getProfiles()
    return Object.entries(allProfiles).reduce((a, [k, v]) => {
      a.push({ value: k, text: v.profileName } as SelectOption)
      return a
    }, [] as SelectOption[])
}

export const getDefaultProfileKey = async ():Promise<string> => {
    const allProfiles = await getProfiles()
    const availableProfileKeys = Object.keys(allProfiles)
  
    // Try user's preferred default first, then fallback to available profiles
    return (allProfiles[getGlobalSettings().defaultProfile || ''] ||
          allProfiles[defaultProfile] ||
          allProfiles[availableProfileKeys[0]]).profile
}

export const getProfile = async (key:string, forReset:boolean = false):Promise<ChatSettings> => {
    const allProfiles = await getProfiles()
    const availableProfileKeys = Object.keys(allProfiles)
  
    let profile = allProfiles[key] ||
    allProfiles[getGlobalSettings().defaultProfile || ''] ||
    allProfiles[defaultProfile] ||
    allProfiles[availableProfileKeys[0]]
  
    if (forReset && isStaticProfile(key)) {
      profile = profiles[key]
    }
    const clone = JSON.parse(JSON.stringify(profile)) // Always return a copy
    Object.keys(getExcludeFromProfile()).forEach(k => {
      delete clone[k]
    })
    return clone
}

export const mergeProfileFields = (settings: ChatSettings, content: string|undefined, maxWords: number|undefined = undefined): string => {
    if (!content?.toString) return ''
    content = (content + '').replaceAll('[[CHARACTER_NAME]]', settings.characterName || 'Assistant')
    if (maxWords) content = (content + '').replaceAll('[[MAX_WORDS]]', maxWords.toString())
    return content
}

export const cleanContent = (settings: ChatSettings, content: string|undefined): string => {
    return (content || '').replace(/::NOTE::[\s\S]*?::NOTE::\s*/g, '')
}

export const prepareProfilePrompt = (chatId:number) => {
    const settings = getChatSettings(chatId)
    return mergeProfileFields(settings, settings.systemPrompt).trim()
}

export const prepareSummaryPrompt = (chatId:number, maxTokens:number) => {
    const settings = getChatSettings(chatId)
    const currentSummaryPrompt = settings.summaryPrompt
    // ~.75 words per token.  We'll use 0.70 for a little extra margin.
    return mergeProfileFields(settings, currentSummaryPrompt, Math.floor(maxTokens * 0.70)).trim()
}

export const setSystemPrompt = (chatId: number) => {
    const messages = getMessages(chatId)
    const systemPromptMessage:Message = {
      role: 'system',
      content: prepareProfilePrompt(chatId),
      uuid: uuidv4()
    }
    if (messages[0]?.role === 'system') deleteMessage(chatId, messages[0].uuid)
    messages.unshift(systemPromptMessage)
    setMessages(chatId, messages.filter(m => true))
}

// Restart currently loaded profile
export const restartProfile = async (chatId:number, noApply:boolean = false): Promise<void> => {
    const settings = getChatSettings(chatId)
    if (!settings.profile && !noApply) return await applyProfile(chatId, '', true)
    // Clear current messages
    clearMessages(chatId)
    // Add the system prompt
    setSystemPrompt(chatId)

    // Add trainingPrompts, if any
    if (settings.trainingPrompts) {
      settings.trainingPrompts.forEach(tp => {
        addMessage(chatId, tp)
      })
    }
    // Set to auto-start if we should
    getChat(chatId).startSession = settings.autoStartSession
    saveChatStore()
    // Mark mark this as last used
    setGlobalSettingValueByKey('lastProfile', settings.profile)
}

export const newNameForProfile = async (name:string) => {
    const profiles = await getProfileSelect()
    return newName(name, profiles.reduce((a: Record<string, SelectOption>, p) => { a[p.text] = p; return a }, {}))
}

// Apply currently selected profile
export const applyProfile = async (chatId:number, key:string = '', resetChat:boolean = false) => {
    await resetChatSettings(chatId, resetChat) // Fully reset
    if (!resetChat) return
    return await restartProfile(chatId, true)
}

const profiles:Record<string, ChatSettings> = {
    gptoss: {
      ...chatDefaults,
      useSystemPrompt: true,
      continuousChat: 'warn-only',
      autoStartSession: false,
      systemPrompt: 'You, gpt-oss, run as part of the AI service Privatemode AI, which was developed by Edgeless Systems. You run inside a secure environment based on confidential computing (AMD SEV-SNP, Nvidia H100). The environment cannot be accessed from the outside and user data remains encrypted in memory during processing. Even Edgeless Systems cannot access the data. You are a helpful assistant answering user questions concisely and to the point. You don\'t talk about yourself unless asked.',
      modelConfig: {
        id: 'openai/gpt-oss-120b',
        displayName: 'gpt-oss 120B',
        displaySubtitle: 'Reasoning model suited for complex tasks',
        reasoningOptions: [
          { value: 'low', displayName: 'Low' },
          { value: 'medium', displayName: 'Medium' },
          { value: 'high', displayName: 'High' }
        ]
      }
    },
    gemma: {
      ...chatDefaults,
      useSystemPrompt: true,
      continuousChat: 'warn-only',
      autoStartSession: false,
      systemPrompt: 'You, Gemma, run as part of the AI service Privatemode AI, which was developed by Edgeless Systems. You run inside a secure environment based on confidential computing (AMD SEV-SNP, Nvidia H100). The environment cannot be accessed from the outside and user data remains encrypted in memory during processing. Even Edgeless Systems cannot access the data. You are a helpful assistant answering user questions concisely and to the point. You don\'t talk about yourself unless asked.',
      modelConfig: {
        id: 'leon-se/gemma-3-27b-it-fp8-dynamic',
        displayName: 'Gemma 3 27B',
        displaySubtitle: 'Multi-modal model with image understanding'
      }
    },
    qwencoder: {
      ...chatDefaults,
      useSystemPrompt: true,
      continuousChat: 'warn-only',
      autoStartSession: false,
      systemPrompt: 'You, Qwen Coder, run as part of the AI service Privatemode AI, which was developed by Edgeless Systems. You run inside a secure environment based on confidential computing (AMD SEV-SNP, Nvidia H100). The environment cannot be accessed from the outside and user data remains encrypted in memory during processing. Even Edgeless Systems cannot access the data. You are a helpful assistant answering user questions concisely and to the point. You don\'t talk about yourself unless asked.',
      modelConfig: {
        id: 'qwen3-coder-30b-a3b',
        displayName: 'Qwen 3 Coder 30B',
        displaySubtitle: 'Coding-specialized model for programming tasks'
      }
    }
}

// Set keys for static profiles
Object.entries(profiles).forEach(([k, v]) => { v.profile = k })

</script>
