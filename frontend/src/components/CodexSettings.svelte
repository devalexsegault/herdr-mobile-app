<script lang="ts">
  import { onMount } from 'svelte';
  import Button from '$components/ui/Button.svelte';
  import { codexChoices, codexChoiceKeys } from '$lib/codex';
  import { stripAnsi } from '$lib/terminal';
  import { relayStore } from '$lib/store';
  import type { Agent } from '$lib/types';

  let { agent, model, effort, mode, section = 'model', locked, onchange }: {
    agent: Agent; model: string; effort: string; mode: string;
    section?: 'model' | 'effort' | 'mode'; locked: boolean;
    onchange: (settings: { model?: string; effort?: string; mode?: string }) => void;
  } = $props();
  const frames = relayStore.terminalFrames;
  const text = $derived(stripAnsi($frames.get(agent.pane_id)?.content || ''));
  const menu = $derived(codexChoices(text));
  let busy = $state(false);
  let error = $state('');
  let status = $state('');
  let selectedModel = '';
  let mounted = false;

  async function refreshAfter(before: string) {
    for (let attempt = 0; attempt < 10 && mounted; attempt += 1) {
      relayStore.readPane(agent, true);
      await new Promise((resolve) => setTimeout(resolve, 400));
      if (text !== before) return;
    }
    if (mounted) throw new Error('Codex has not refreshed its choices yet. Try refreshing.');
  }

  async function openModels() {
    if (busy || locked) return;
    if (menu?.kind === 'model' || menu?.kind === 'effort') return;
    busy = true;
    error = '';
    status = 'Loading available models…';
    try {
      const before = text;
      await relayStore.sendToAgent(agent, { type: 'submit_prompt', text: '/model' });
      await refreshAfter(before);
      status = '';
      const refreshed = codexChoices(text);
      if (refreshed?.kind !== 'model' && refreshed?.kind !== 'effort') throw new Error('Model choices are unavailable. Refresh to try again.');
    } catch (failure) {
      error = failure instanceof Error ? failure.message : 'Could not load models.';
      status = '';
    } finally { busy = false; }
  }

  async function choose(index: number) {
    if (busy || !menu || menu.kind === 'question' || locked) return;
    const snapshot = menu;
    const keys = codexChoiceKeys(snapshot, index);
    if (!keys.length) return;
    busy = true;
    error = '';
    const label = snapshot.choices[index].label.replace(/\s*\((?:current|default|recommended)\)/gi, '').trim();
    try {
      const before = text;
      await relayStore.sendToAgent(agent, { type: 'send_keys', keys, activity_label: `Selected ${label}` });
      await refreshAfter(before);
      if (snapshot.kind === 'model') selectedModel = label;
      if (!menu) {
        onchange({ ...(selectedModel ? { model: selectedModel } : {}), ...(snapshot.kind === 'effort' ? { effort: label } : {}) });
        status = 'Settings updated.';
      }
    } catch (failure) {
      error = failure instanceof Error ? failure.message : 'The setting could not be changed.';
    } finally { busy = false; }
  }

  async function selectMode(target: 'default' | 'plan') {
    if (busy || locked || menu || target === mode) return;
    if (target === 'default' && (mode !== 'plan' || !/plan mode/i.test(text.slice(-500)))) {
      error = 'The current mode is unknown. Refresh the agent before switching to Default.';
      return;
    }
    busy = true;
    error = '';
    try {
      const before = text;
      await relayStore.sendToAgent(agent, target === 'plan'
        ? { type: 'submit_prompt', text: '/plan' }
        : { type: 'send_keys', keys: ['shift+tab'], activity_label: 'Switch to Default mode' });
      await refreshAfter(before);
      if (target === 'plan' && !/plan mode/i.test(text.slice(-500))) throw new Error('Codex has not confirmed Plan mode. Refresh and try again.');
      if (target === 'default' && /plan mode/i.test(text.slice(-500))) throw new Error('Codex still reports Plan mode. Refresh and try again.');
      onchange({ mode: target });
      status = `Mode set to ${target === 'plan' ? 'Plan' : 'Default'}.`;
    } catch (failure) { error = failure instanceof Error ? failure.message : 'The mode could not be changed.'; }
    finally { busy = false; }
  }

  async function cancelMenu() {
    if (busy || !menu || menu.kind === 'question' || locked) return;
    busy = true;
    error = '';
    try {
      const before = text;
      await relayStore.sendToAgent(agent, { type: 'send_keys', keys: ['Escape'], activity_label: 'Cancel settings selection' });
      await refreshAfter(before);
    } catch (failure) { error = failure instanceof Error ? failure.message : 'Could not close the selection.'; }
    finally { busy = false; }
  }

  onMount(() => {
    mounted = true;
    if (section !== 'mode') void (async () => {
      await openModels();
      const current = codexChoices(text);
      if (!mounted || section !== 'effort' || current?.kind !== 'model') return;
      const index = current.choices.findIndex((choice) => choice.current || choice.label === model);
      if (index >= 0) await choose(index);
    })();
    return () => { mounted = false; };
  });
</script>

<div class="form-stack" aria-busy={busy}>
  <h3 class="settings-section-title">Mode</h3>
  <div class="choice-grid settings-grid" role="group" aria-label="Mode">
    {#each [{ id: 'default', label: 'Default', hint: 'Work on the task with the configured permissions.' }, { id: 'plan', label: 'Plan', hint: 'Discuss and prepare a plan before implementation.' }] as choice (choice.id)}
      <button type="button" class="choice" class:active={mode === choice.id} aria-pressed={mode === choice.id}
        disabled={busy || locked || Boolean(menu)} onclick={() => { void selectMode(choice.id as 'default' | 'plan'); }}>
        <strong>{choice.label}</strong><small>{choice.hint}</small>
      </button>
    {/each}
  </div>
  <h3 class="settings-section-title">Model</h3>
  <p>{model || 'Not recorded yet'}</p>
  {#if menu && menu.kind !== 'question'}
    <h3>{menu.kind === 'effort' ? 'Reasoning effort' : menu.title}</h3>
    <div class="choice-grid settings-grid" role="group" aria-label={menu.kind === 'effort' ? 'Reasoning effort' : 'Model'}>
      {#each menu.choices as choice, index (index)}
        <button type="button" class="choice" class:active={choice.current} aria-pressed={choice.current}
          disabled={busy || locked} onclick={() => { void choose(index); }}>
          <strong>{choice.label}</strong><small>{choice.description}</small>
        </button>
      {/each}
    </div>
    <Button variant="secondary" disabled={busy || locked} onclick={() => { void cancelMenu(); }}>Back</Button>
  {:else}
    <Button variant="secondary" disabled={busy || locked} onclick={() => { void openModels(); }}>Choose model and reasoning effort</Button>
  {/if}
  {#if menu?.kind !== 'effort'}
    <h3 class="settings-section-title">Reasoning effort</h3>
    <p>{effort || 'Not recorded yet'}</p>
    <p class="hint">Choose a model to see its available reasoning levels.</p>
  {/if}
  {#if locked}<p role="status">Finish the current turn or answer the pending question to change settings.</p>{/if}
  {#if status}<p role="status">{status}</p>{/if}
  {#if error}<p role="alert">{error}</p>{/if}
  <Button variant="ghost" disabled={busy} onclick={() => { relayStore.readPane(agent, true); error = ''; }}>Refresh</Button>
</div>
