<script lang="ts">
  import { onMount } from 'svelte';
  import ChevronDown from '@lucide/svelte/icons/chevron-down';

  type Option<T extends string> = {
    value: T;
    label: string;
  };

  let {
    label,
    value,
    options,
    disabled = false,
    onChange
  }: {
    label: string;
    value: string;
    options: Option<string>[];
    disabled?: boolean;
    onChange: (value: string) => void;
  } = $props();

  let open = $state(false);
  let root = $state<HTMLDivElement | null>(null);
  const selected = $derived(options.find((option) => option.value === value) ?? options[0]);

  onMount(() => {
    const close = (event: MouseEvent) => {
      if (root && !root.contains(event.target as Node)) {
        open = false;
      }
    };
    document.addEventListener('click', close);
    return () => {
      document.removeEventListener('click', close);
    };
  });

  function choose(nextValue: string) {
    onChange(nextValue);
    open = false;
  }
</script>

<div class="select-menu" bind:this={root}>
  <span>{label}</span>
  <button
    type="button"
    class="select-trigger"
    aria-haspopup="listbox"
    aria-expanded={open}
    {disabled}
    onclick={() => {
      if (!disabled) open = !open;
    }}
    onkeydown={(event) => {
      if (event.key === 'Escape') open = false;
    }}
  >
    {selected?.label ?? 'Select'}
    <ChevronDown size={16} />
  </button>
  {#if open}
    <div class="select-popover" role="listbox" aria-label={label}>
      {#each options as option (option.value)}
        <button
          type="button"
          class:active={option.value === value}
          role="option"
          aria-selected={option.value === value}
          onclick={() => choose(option.value)}
        >
          {option.label}
        </button>
      {/each}
    </div>
  {/if}
</div>
