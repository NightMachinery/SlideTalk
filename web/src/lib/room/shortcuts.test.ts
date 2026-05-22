import { describe, expect, it } from 'vitest';
import {
  defaultShortcutBindings,
  loadShortcutConfig,
  resetShortcutBinding,
  resolveShortcutAction,
  saveShortcutConfig,
  setShortcutBinding,
  shouldIgnoreShortcut
} from './shortcuts';

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

  it('allows slide navigation shortcuts outside editable fields', () => {
    expect(shouldIgnoreShortcut(new KeyboardEvent('keydown', { key: '[' }), document.body)).toBe(false);
    expect(shouldIgnoreShortcut(new KeyboardEvent('keydown', { key: ']' }), document.body)).toBe(false);
  });
});

describe('shortcut configuration', () => {
  it('maps default keys to room actions', () => {
    const config = loadShortcutConfig();

    expect(resolveShortcutAction(new KeyboardEvent('keydown', { key: 'b' }), config)).toBe('previousSpeaker');
    expect(resolveShortcutAction(new KeyboardEvent('keydown', { key: 'n' }), config)).toBe('nextSpeaker');
    expect(resolveShortcutAction(new KeyboardEvent('keydown', { key: 't' }), config)).toBe('toggleTimer');
    expect(resolveShortcutAction(new KeyboardEvent('keydown', { key: '[' }), config)).toBe('previousSlide');
    expect(resolveShortcutAction(new KeyboardEvent('keydown', { key: ']' }), config)).toBe('nextSlide');
    expect(resolveShortcutAction(new KeyboardEvent('keydown', { key: '?' }), config)).toBe('toggleHelp');
    expect(resolveShortcutAction(new KeyboardEvent('keydown', { key: '/', shiftKey: true }), config)).toBe('toggleHelp');
  });

  it('keeps shifted help shortcut suppressed in editable fields', () => {
    document.body.innerHTML = '<input id="shortcut-source" />';
    const input = document.getElementById('shortcut-source');

    expect(shouldIgnoreShortcut(new KeyboardEvent('keydown', { key: '/', shiftKey: true }), input)).toBe(true);
  });

  it('persists custom shortcut bindings locally', () => {
    const updated = setShortcutBinding(loadShortcutConfig(), 'nextSpeaker', 'j');

    expect(updated.error).toBe('');
    saveShortcutConfig(updated.config);

    expect(loadShortcutConfig().bindings.nextSpeaker).toBe('j');
    expect(resolveShortcutAction(new KeyboardEvent('keydown', { key: 'j' }), loadShortcutConfig())).toBe('nextSpeaker');
    expect(resolveShortcutAction(new KeyboardEvent('keydown', { key: 'n' }), loadShortcutConfig())).toBe(null);
  });

  it('rejects duplicate and fixed help shortcut bindings', () => {
    const config = loadShortcutConfig();

    expect(setShortcutBinding(config, 'nextSpeaker', 'b').error).toContain('already used');
    expect(setShortcutBinding(config, 'nextSpeaker', '?').error).toContain('reserved');
  });

  it('resets an empty binding to its default', () => {
    const custom = setShortcutBinding(loadShortcutConfig(), 'nextSpeaker', 'j').config;
    const reset = resetShortcutBinding(custom, 'nextSpeaker');

    expect(reset.bindings.nextSpeaker).toBe(defaultShortcutBindings.nextSpeaker);
  });
});
