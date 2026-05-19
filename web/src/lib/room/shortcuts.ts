export function shouldIgnoreShortcut(event: KeyboardEvent, target: EventTarget | null = event.target): boolean {
  if (event.key === '[' || event.key === ']') {
    return true;
  }
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  const tagName = target.tagName.toLowerCase();
  return tagName === 'input' || tagName === 'textarea' || target.isContentEditable === true || target.closest('[contenteditable="true"],[contenteditable=""]') !== null;
}
