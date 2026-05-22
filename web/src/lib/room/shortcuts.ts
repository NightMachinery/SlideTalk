export const shortcutStorageKey = 'slidetalk.shortcuts.v1';

export type ShortcutAction = 'previousSpeaker' | 'nextSpeaker' | 'toggleTimerPause' | 'resetAndStartTimer' | 'previousSlide' | 'nextSlide' | 'toggleHelp';
export type RebindableShortcutAction = Exclude<ShortcutAction, 'toggleHelp'>;
export type ShortcutBindings = Record<RebindableShortcutAction, string>;
export type ShortcutConfig = {
  enabled: boolean;
  modShortcutsEnabled: boolean;
  bindings: ShortcutBindings;
};

export const defaultShortcutBindings: ShortcutBindings = {
  previousSpeaker: 'b',
  nextSpeaker: 'n',
  toggleTimerPause: 'p',
  resetAndStartTimer: 't',
  previousSlide: '[',
  nextSlide: ']'
};

export const shortcutLabels: Record<ShortcutAction, string> = {
  previousSpeaker: 'Previous speaker',
  nextSpeaker: 'Next speaker',
  toggleTimerPause: 'Start/pause timer',
  resetAndStartTimer: 'Reset and start timer',
  previousSlide: 'Previous slide',
  nextSlide: 'Next slide',
  toggleHelp: 'Shortcuts'
};

const fixedHelpKey = '?';
const rebindableActions = Object.keys(defaultShortcutBindings) as RebindableShortcutAction[];

export function shouldIgnoreShortcut(event: KeyboardEvent, target: EventTarget | null = event.target): boolean {
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  const tagName = target.tagName.toLowerCase();
  return tagName === 'input' || tagName === 'textarea' || tagName === 'select' || target.isContentEditable === true || target.closest('[contenteditable="true"],[contenteditable=""]') !== null;
}

export function loadShortcutConfig(storage: Storage | null = browserStorage()): ShortcutConfig {
  const fallback = defaultShortcutConfig();
  if (!storage) return fallback;
  const raw = storage.getItem(shortcutStorageKey);
  if (!raw) return fallback;
  try {
    const parsed = JSON.parse(raw) as Partial<ShortcutConfig>;
    return normalizeShortcutConfig(parsed);
  } catch {
    return fallback;
  }
}

export function saveShortcutConfig(config: ShortcutConfig, storage: Storage | null = browserStorage()) {
  if (!storage) return;
  storage.setItem(shortcutStorageKey, JSON.stringify(normalizeShortcutConfig(config)));
}

export function setShortcutBinding(config: ShortcutConfig, action: RebindableShortcutAction, key: string): { config: ShortcutConfig; error: string } {
  const normalizedKey = normalizeKey(key);
  if (!normalizedKey) {
    return { config: resetShortcutBinding(config, action), error: '' };
  }
  if (normalizedKey === fixedHelpKey) {
    return { config, error: `${formatKey(fixedHelpKey)} is reserved for the shortcuts panel.` };
  }
  for (const candidate of rebindableActions) {
    if (candidate !== action && config.bindings[candidate] === normalizedKey) {
      return { config, error: `${formatKey(normalizedKey)} is already used for ${shortcutLabels[candidate]}.` };
    }
  }
  return {
    config: {
      ...config,
      bindings: {
        ...config.bindings,
        [action]: normalizedKey
      }
    },
    error: ''
  };
}

export function resetShortcutBinding(config: ShortcutConfig, action: RebindableShortcutAction): ShortcutConfig {
  return {
    ...config,
    bindings: {
      ...config.bindings,
      [action]: defaultShortcutBindings[action]
    }
  };
}

export function resolveShortcutAction(event: KeyboardEvent, config: ShortcutConfig): ShortcutAction | null {
  const key = normalizeEventKey(event);
  if (!key) return null;
  if (key === fixedHelpKey) return 'toggleHelp';
  if (!config.enabled || !config.modShortcutsEnabled) return null;
  for (const action of rebindableActions) {
    if (config.bindings[action] === key) return action;
  }
  return null;
}

export function formatKey(key: string): string {
  return key.length === 1 ? key.toUpperCase() : key;
}

export function defaultShortcutConfig(): ShortcutConfig {
  return {
    enabled: true,
    modShortcutsEnabled: true,
    bindings: { ...defaultShortcutBindings }
  };
}

function normalizeShortcutConfig(config: Partial<ShortcutConfig>): ShortcutConfig {
  const fallback = defaultShortcutConfig();
  return {
    enabled: typeof config.enabled === 'boolean' ? config.enabled : fallback.enabled,
    modShortcutsEnabled: typeof config.modShortcutsEnabled === 'boolean' ? config.modShortcutsEnabled : fallback.modShortcutsEnabled,
    bindings: normalizeBindings(config.bindings)
  };
}

function normalizeBindings(bindings: Partial<ShortcutBindings> | undefined): ShortcutBindings {
  const normalized = { ...defaultShortcutBindings };
  const used = new Set(Object.values(normalized));
  for (const action of rebindableActions) {
    const key = normalizeKey(bindings?.[action] ?? '');
    if (!key || key === fixedHelpKey || used.has(key)) continue;
    used.delete(normalized[action]);
    normalized[action] = key;
    used.add(key);
  }
  return normalized;
}

function normalizeKey(key: string): string {
  const trimmed = key.trim();
  if (!trimmed) return '';
  if (trimmed.length === 1) return trimmed.toLowerCase();
  return trimmed;
}

function normalizeEventKey(event: KeyboardEvent): string {
  if (event.shiftKey && event.key === '/') return fixedHelpKey;
  return normalizeKey(event.key);
}

function browserStorage(): Storage | null {
  if (typeof localStorage === 'undefined') return null;
  return localStorage;
}
