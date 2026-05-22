<script lang="ts">
  import { toastState } from './toast.svelte';
</script>

{#if toastState.items.length > 0}
  <div class="toast-list" aria-live="polite">
    {#each toastState.items as toast (toast.id)}
      <div class={['toast-item', `toast-${toast.type}`]} role="alert">
        {toast.message}
      </div>
    {/each}
  </div>
{/if}

<style>
  .toast-list {
    position: fixed;
    z-index: 100010;
    top: 16px;
    left: 50%;
    display: grid;
    gap: 8px;
    width: min(420px, calc(100vw - 24px));
    pointer-events: none;
    transform: translateX(-50%);
  }

  .toast-item {
    border-radius: 8px;
    padding: 10px 14px;
    color: white;
    font-size: 0.9rem;
    font-weight: 720;
    box-shadow: 0 18px 48px oklch(24% 0.04 245 / 0.24);
    pointer-events: auto;
  }

  .toast-error {
    background: oklch(48% 0.17 28);
  }

  .toast-success {
    background: oklch(45% 0.12 158);
  }
</style>
