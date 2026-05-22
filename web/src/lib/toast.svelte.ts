export interface Toast {
  id: number;
  message: string;
  type: 'error' | 'success';
}

export const toastState = $state({
  items: [] as Toast[]
});

let nextId = 0;

export function addToast(message: string, type: 'error' | 'success' = 'error', duration = 5000) {
  const id = nextId++;
  toastState.items.push({ id, message, type });
  window.setTimeout(() => {
    toastState.items = toastState.items.filter((toast) => toast.id !== id);
  }, duration);
}
