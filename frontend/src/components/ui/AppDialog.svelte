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

  // Drag to dismiss: the sheet follows the finger downward and closes past a
  // third of its height or on a quick flick; otherwise it springs back. A drag
  // only starts on the grabber/title, or on content scrolled to its top, so
  // scrolling a long sheet never fights the gesture.
  let content = $state<HTMLDivElement>();
  let dragging = $state(false);
  let offset = $state(0);
  let dragStartY = 0;
  let dragStartAt = 0;
  let dragArmed = false;
  function dragStart(event: PointerEvent) {
    if (presentation !== 'sheet' || !dismissible || event.pointerType === 'mouse') return;
    const target = event.target as HTMLElement | null;
    const scrollable = target?.closest('.dialog-content');
    dragArmed = !scrollable || scrollable.scrollTop <= 0 || Boolean(target?.closest('.sheet-grabber, .dialog-title'));
    dragStartY = event.clientY;
    dragStartAt = performance.now();
    offset = 0;
  }
  function dragMove(event: PointerEvent) {
    if (!dragArmed || presentation !== 'sheet') return;
    const dy = event.clientY - dragStartY;
    if (!dragging && dy > 8) dragging = true;
    if (dragging) {
      offset = Math.max(0, dy);
      event.preventDefault();
    }
  }
  function dragEnd(event: PointerEvent) {
    if (!dragArmed) return;
    dragArmed = false;
    if (!dragging) return;
    dragging = false;
    const height = content?.offsetHeight || 400;
    const elapsed = Math.max(1, performance.now() - dragStartAt);
    const velocity = offset / elapsed;
    if (offset > height / 3 || velocity > 0.6) {
      offset = height;
      setTimeout(() => { open = false; offset = 0; }, 160);
    } else {
      offset = 0;
    }
    event.preventDefault();
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
    <div
      class="dialog-content"
      class:dragging
      role="presentation"
      bind:this={content}
      style={presentation === 'sheet' && offset ? `transform: translateY(${offset}px)` : undefined}
      onpointerdown={dragStart}
      onpointermove={dragMove}
      onpointerup={dragEnd}
      onpointercancel={dragEnd}
    >
      {#if presentation === 'sheet'}<span class="sheet-grabber" aria-hidden="true"></span>{/if}
      <h2 class="dialog-title" id={`${id}-title`}>{title}</h2>
      {#if description}<p class="dialog-description" id={`${id}-description`}>{description}</p>{/if}
      {@render children?.()}
    </div>
  </dialog>
{/if}
