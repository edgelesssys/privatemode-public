import { marked } from 'marked';
import DOMPurify from 'dompurify';
import type { Tokens } from 'marked';
import hljs from 'highlight.js';

const renderer = new marked.Renderer();

// XSS-prone on itself, but fine due to DOMPurify sanitization later.
renderer.code = function (token: Tokens.Code) {
  const lang = token.lang || '';
  let highlightedCode: string;

  try {
    if (lang && hljs.getLanguage(lang)) {
      highlightedCode = hljs.highlight(token.text, { language: lang }).value;
    } else {
      highlightedCode = hljs.highlightAuto(token.text).value;
    }
  } catch {
    highlightedCode = token.text;
  }

  const copyButton = `<button class="copy-button" data-code="${encodeURIComponent(token.text)}"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg><span class="copy-text">Copy</span><span class="copied-text">Copied!</span></button>`;
  return `<div class="code-block-wrapper">${copyButton}<pre><code class="hljs${lang ? ` language-${lang}` : ''}">${highlightedCode}</code></pre></div>`;
};

marked.setOptions({
  breaks: true,
  gfm: true,
  renderer,
});

const markdownCache = new Map<string, string>();
const MAX_CACHE_SIZE = 1000;

export function renderMarkdown(content: string): string {
  if (!content) return '';

  if (markdownCache.has(content)) {
    return markdownCache.get(content)!;
  }

  const rawHtml = marked.parse(content, { async: false }) as string;
  const cleanHtml = DOMPurify.sanitize(rawHtml, {
    ADD_ATTR: ['data-code'],
  });

  if (rawHtml !== cleanHtml) {
    console.warn(
      'Markdown content was sanitized to remove potentially unsafe HTML.',
      rawHtml,
      cleanHtml,
    );
  }

  if (markdownCache.size >= MAX_CACHE_SIZE) {
    const firstKey = markdownCache.keys().next().value;
    if (firstKey !== undefined) {
      markdownCache.delete(firstKey);
    }
  }
  markdownCache.set(content, cleanHtml);

  return cleanHtml;
}
