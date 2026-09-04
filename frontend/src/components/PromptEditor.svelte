<script lang="ts">
  import Button from '$components/ui/Button.svelte';

  /**
   * A full-screen editor for a long prompt. A column's system prompt runs to
   * thousands of characters; inside a sheet's five-row textarea it can be
   * neither read nor edited on a phone, so it gets the whole screen: the text
   * fills the height, the keyboard takes the bottom, and nothing else
   * competes for room.
   */
  let {
    open = $bindable(false),
    title,
    value = $bindable(''),
    placeholder = '',
    onsave,
  }: {
    open?: boolean;
    title: string;
    value?: string;
    placeholder?: string;
    onsave?: (value: string) => void;
  } = $props();

  let draft = $state('');
  let field = $state<HTMLTextAreaElement>(null!);
  let wrap = $state(true);
  // A modal <dialog>, not a fixed div: the sheet that opens this editor is
  // itself modal, and only the top layer sits above another modal.
  let host = $state<HTMLDialogElement>();

  $effect(() => {
    if (!open) return;
    draft = value;
    if (host && !host.open) host.showModal();
    void Promise.resolve().then(() => field?.focus());
  });

  const lines = $derived(draft ? draft.split('\n').length : 0);
  const characters = $derived(draft.length);

  function save() {
    value = draft;
    onsave?.(draft);
    open = false;
  }

  function keydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.preventDefault();
      open = false;
      return;
    }
    if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      save();
    }
  }
</script>

{#if open}
  <dialog
    bind:this={host}
    class="prompt-editor"
    aria-label={title}
    oncancel={(event) => { event.preventDefault(); open = false; }}
    onclose={() => { open = false; }}
  >
    <header class="prompt-editor-bar">
      <Button variant="ghost" size="sm" onclick={() => { open = false; }}>Cancel</Button>
      <span class="prompt-editor-title">{title}</span>
      <Button size="sm" onclick={save}>Done</Button>
    </header>
    <textarea
      bind:this={field}
      bind:value={draft}
      class="prompt-editor-field"
      class:nowrap={!wrap}
      {placeholder}
      aria-label={title}
      autocapitalize="sentences"
      spellcheck="true"
      onkeydown={keydown}
    ></textarea>
    <footer class="prompt-editor-foot">
      <span>{lines} {lines === 1 ? 'line' : 'lines'} · {characters} characters</span>
      <button type="button" class="link-button" aria-pressed={!wrap} onclick={() => { wrap = !wrap; }}>
        {wrap ? 'Stop wrapping' : 'Wrap lines'}
      </button>
    </footer>
  </dialog>
{/if}
