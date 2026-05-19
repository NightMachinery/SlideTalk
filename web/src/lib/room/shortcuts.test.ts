import { describe, expect, it } from 'vitest';
import { shouldIgnoreShortcut } from './shortcuts';

describe('shouldIgnoreShortcut', () => {
  it('ignores shortcuts while text fields are focused', () => {
    document.body.innerHTML = `
      <input id="name" />
      <textarea id="notes"></textarea>
      <div id="editor" contenteditable="true"></div>
      <button id="button">Next</button>
    `;

    expect(shouldIgnoreShortcut(new KeyboardEvent('keydown', { key: 'n' }), document.getElementById('name'))).toBe(true);
    expect(shouldIgnoreShortcut(new KeyboardEvent('keydown', { key: 'n' }), document.getElementById('notes'))).toBe(true);
    expect(shouldIgnoreShortcut(new KeyboardEvent('keydown', { key: 'n' }), document.getElementById('editor'))).toBe(true);
    expect(shouldIgnoreShortcut(new KeyboardEvent('keydown', { key: 'n' }), document.getElementById('button'))).toBe(false);
  });

  it('reserves slide navigation shortcuts', () => {
    expect(shouldIgnoreShortcut(new KeyboardEvent('keydown', { key: '[' }), document.body)).toBe(true);
    expect(shouldIgnoreShortcut(new KeyboardEvent('keydown', { key: ']' }), document.body)).toBe(true);
  });
});
