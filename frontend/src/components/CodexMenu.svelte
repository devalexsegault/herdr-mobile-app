<script lang="ts">
  import { codexChoices, codexChoiceKeys } from '$lib/codex';
  import Button from '$components/ui/Button.svelte';
  import { relayStore } from '$lib/store';
  import type { Agent } from '$lib/types';
  import type { TerminalMenu, TerminalMenuAction } from '$lib/terminal-menu';

  let { agent, menu, text }: { agent: Agent; menu: TerminalMenu; text: string } = $props();
  let busy = $state(false);
  let error = $state('');
  const choices = $derived(codexChoices(text));
  const actions = $derived([
    ...(['Up', 'Down'] as const).filter((key) => !menu.actions.some((action) => action.keys.includes(key)))
      .map((key) => ({ label: key, keys: [key], cancel: false })),
    ...(choices?.kind === 'question' && /tab to add notes/i.test(text) ? [{ label: 'Add details', keys: ['Tab'], cancel: false }] : []),
    ...menu.actions,
  ]);

  async function act(action: TerminalMenuAction) {
    if (busy) return;
    busy = true;
    error = '';
    try {
      await relayStore.sendToAgent(agent, { type: 'send_keys', keys: action.keys, activity_label: action.label });
      relayStore.readPane(agent, true);
    } catch (failure) {
      error = failure instanceof Error ? failure.message : 'The selection could not be sent.';
    } finally {
      busy = false;
    }
  }
</script>

<section class="conversation-answer" aria-label="Codex menu" aria-busy={busy}>
  <h3>{choices?.title || menu.title}</h3>
  {#if choices}
    <div class="choice-grid settings-grid" role="group" aria-label={choices.kind === 'question' ? 'Answer' : choices.kind}>
      {#each choices.choices as choice, index (index)}
        <button type="button" class="choice" class:active={choice.current} aria-pressed={choice.current} disabled={busy}
          onclick={() => { if (choices) void act({ keys: codexChoiceKeys(choices, index), label: choice.label, cancel: false }); }}>
          <strong>{choice.label}</strong><small>{choice.description}</small>
        </button>
      {/each}
    </div>
  {:else}
    <pre>{text.trimEnd().split('\n').slice(-30).join('\n')}</pre>
  {/if}
  <div class="conversation-answer-actions">
    {#each actions as action (action.keys.join('+'))}
      <Button variant={action.cancel ? 'secondary' : 'default'} disabled={busy} onclick={() => { void act(action); }}>{action.label}</Button>
    {/each}
  </div>
  {#if error}<p role="alert">{error}</p>{/if}
</section>

<style>
  pre { max-height: 18rem; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; }
</style>
