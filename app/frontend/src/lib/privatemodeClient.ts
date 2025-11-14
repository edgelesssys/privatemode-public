import { getApiKey } from './apiKey';
import type { Message } from './chatStore';

export interface Model {
  id: string;
  object: string;
  created: number;
  owned_by: string;
}

export interface ModelsResponse {
  object: string;
  data: Model[];
}

export interface ChatCompletionMessage {
  role: 'user' | 'assistant' | 'system';
  content: string;
}

export interface ChatCompletionRequest {
  model: string;
  messages: ChatCompletionMessage[];
  stream: true;
  reasoning_effort?: 'low' | 'medium' | 'high';
}

export interface ChatCompletionChunk {
  id: string;
  object: string;
  created: number;
  model: string;
  choices: Array<{
    index: number;
    delta: {
      role?: string;
      content?: string;
    };
    finish_reason: string | null;
  }>;
}

export interface UnstructuredElement {
  type: string;
  element_id: string;
  text: string;
  metadata: {
    languages?: string[];
    page_number?: number;
    filename?: string;
    filetype?: string;
  };
}

export class PrivatemodeClient {
  private baseUrl: string;
  private apiKey: string | null;

  constructor(proxyPort: string) {
    this.baseUrl = `http://localhost:${proxyPort}`;
    this.apiKey = getApiKey();
  }

  async fetchModels(): Promise<Model[]> {
    if (!this.apiKey) {
      throw new Error('API key not configured');
    }

    const response = await fetch(`${this.baseUrl}/v1/models`, {
      headers: {
        Authorization: `Bearer ${this.apiKey}`,
        'Content-Type': 'application/json',
      },
    });

    if (!response.ok) {
      throw new Error(
        `Failed to fetch models: ${response.status} ${response.statusText}`,
      );
    }

    const data: ModelsResponse = await response.json();
    return data.data;
  }

  async *streamChatCompletion(
    model: string,
    messages: Message[],
    signal?: AbortSignal,
    reasoningEffort?: 'low' | 'medium' | 'high',
    systemPrompt?: string,
  ): AsyncGenerator<string, void, unknown> {
    if (!this.apiKey) {
      throw new Error('API key not configured');
    }

    const apiMessages: ChatCompletionMessage[] = [];

    if (systemPrompt) {
      apiMessages.push({
        role: 'system',
        content: systemPrompt,
      });
    }
    for (const msg of messages) {
      if (msg.attachedFiles && msg.attachedFiles.length > 0) {
        for (const file of msg.attachedFiles) {
          apiMessages.push({
            role: msg.role,
            content: `[File: ${file.name}]\n\n${file.content}`,
          });
        }
      }
      apiMessages.push({
        role: msg.role,
        content: msg.content,
      });
    }

    const requestBody: ChatCompletionRequest = {
      model,
      messages: apiMessages,
      stream: true,
      ...(reasoningEffort && { reasoning_effort: reasoningEffort }),
    };

    const response = await fetch(`${this.baseUrl}/v1/chat/completions`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${this.apiKey}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(requestBody),
      signal,
    });

    if (!response.ok) {
      const error = new Error(
        `Chat completion failed: ${response.status} ${response.statusText}`,
      );
      (error as any).status = response.status;
      throw error;
    }

    if (!response.body) {
      throw new Error('Response body is null');
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';

    try {
      while (true) {
        const { done, value } = await reader.read();

        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || '';

        for (const line of lines) {
          const trimmed = line.trim();
          if (!trimmed || trimmed === 'data: [DONE]') continue;

          if (trimmed.startsWith('data: ')) {
            const data = trimmed.slice(6);
            try {
              const chunk: ChatCompletionChunk = JSON.parse(data);
              const content = chunk.choices[0]?.delta?.content;
              if (content) {
                yield content;
              }
            } catch (e) {
              console.error('Failed to parse chunk:', data, e);
            }
          }
        }
      }
    } finally {
      reader.releaseLock();
    }
  }

  async uploadFile(file: File): Promise<UnstructuredElement[]> {
    if (!this.apiKey) {
      throw new Error('API key not configured');
    }

    const formData = new FormData();
    formData.append('strategy', 'fast');
    formData.append('files', file);

    const response = await fetch(
      `${this.baseUrl}/unstructured/general/v0/general`,
      {
        method: 'POST',
        headers: {
          Authorization: `Bearer ${this.apiKey}`,
        },
        body: formData,
      },
    );

    if (!response.ok) {
      const error = new Error(
        `File upload failed: ${response.status} ${response.statusText}`,
      );
      (error as any).status = response.status;
      throw error;
    }

    return await response.json();
  }
}
