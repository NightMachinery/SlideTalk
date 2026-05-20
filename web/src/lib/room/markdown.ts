export function renderMarkdown(markdown: string): string {
  const blocks = markdown.replace(/\r\n/g, '\n').split(/\n{2,}/);
  return blocks
    .map((block) => renderBlock(block.trim()))
    .filter(Boolean)
    .join('');
}

export type MarkdownBlock = {
  kind: 'h1' | 'h2' | 'p';
  text: string;
};

export function parseMarkdown(markdown: string): MarkdownBlock[] {
  return markdown
    .replace(/\r\n/g, '\n')
    .split(/\n{2,}/)
    .map((block) => block.trim())
    .filter(Boolean)
    .map((block) => {
      if (block.startsWith('# ')) return { kind: 'h1', text: block.slice(2) };
      if (block.startsWith('## ')) return { kind: 'h2', text: block.slice(3) };
      return { kind: 'p', text: block };
    });
}

function renderBlock(block: string): string {
  if (block === '') return '';
  const escaped = escapeHTML(block);
  if (escaped.startsWith('# ')) {
    return `<h1>${inlineMarkdown(escaped.slice(2))}</h1>`;
  }
  if (escaped.startsWith('## ')) {
    return `<h2>${inlineMarkdown(escaped.slice(3))}</h2>`;
  }
  const lines = escaped
    .split('\n')
    .map((line) => inlineMarkdown(line))
    .join('<br>');
  return `<p>${lines}</p>`;
}

function inlineMarkdown(value: string): string {
  return value.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
}

function escapeHTML(value: string): string {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}
