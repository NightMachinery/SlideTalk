import { describe, expect, it } from 'vitest';
import { renderMarkdown } from './markdown';

describe('renderMarkdown', () => {
  it('escapes script injection while rendering basic markdown', () => {
    const html = renderMarkdown('# Notes\n\n<script>alert(1)</script>\n\n**safe**');

    expect(html).toContain('<h1>Notes</h1>');
    expect(html).toContain('&lt;script&gt;alert(1)&lt;/script&gt;');
    expect(html).toContain('<strong>safe</strong>');
    expect(html).not.toContain('<script>');
  });
});
