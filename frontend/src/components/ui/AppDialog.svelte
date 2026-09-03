<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    id,
    open = $bindable(false),
    title,
    description = '',
    children,
    dismissible = true,
    presentation = 'sheet',
  }: {
    id: string;
    open?: boolean;
    title: string;
    description?: string;
    children?: Snippet;
    dismissible?: boolean;
    /** A bottom sheet on phones (the default), or a centred card for gates. */
    presentation?: 'sheet' | 'center';
  } = $props();

  let dialog = $state<HTMLDialogElement>();

  $effect(() => {
    if (!open || !dialog || dialog.open) return;
    dialog.showModal();
  });

  function cancel(event: Event) {
    if (!dismissible) {
      event.preventDefault();
      return;
    }
    open = false;
  }

  function closed() {
    open = false;
  }

  function dismissFromBackdrop(event: MouseEvent) {
    if (!dismissible || event.target !== dialog) return;
    open = false;
  }
</script>

{#if open}
  <dialog
    bind:this={dialog}
    {id}
    class="app-dialog"
    class:sheet={presentation === 'sheet'}
    aria-labelledby={`${id}-title`}
    aria-describedby={description ? `${id}-description` : undefined}
    oncancel={cancel}
    onclose={closed}
    onclick={dismissFromBackdrop}
  >
    <div class="dialog-content">
      {#if presentation === 'sheet'}<span class="sheet-grabber" aria-hidden="true"></span>{/if}
      <h2 class="dialog-title" id={`${id}-title`}>{title}</h2>
      {#if description}<p class="dialog-description" id={`${id}-description`}>{description}</p>{/if}
      {@render children?.()}
    </div>
  </dialog>
{/if}
