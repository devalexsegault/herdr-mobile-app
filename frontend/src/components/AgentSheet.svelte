<script lang="ts">
  import AppDialog from '$components/ui/AppDialog.svelte';
  import Button from '$components/ui/Button.svelte';
  import { hostLabel, tabName } from '$lib/agents';
  import { navigate, replaceView } from '$lib/router';
  import { relayStore } from '$lib/store';
  import type { Agent } from '$lib/types';

  // The agent sheet: what a person reaches by tapping the title in the
  // conversation or long-pressing a row on Today. Renaming is its first job;
  // the rest are the actions that used to hide behind the Manage dialog.
  let { open = $bindable(false), agent }: { open?: boolean; agent: Agent | null } = $props();

  let name = $state('');
  let busy = $state(false);
  let error = $state('');
  let confirmingStop = $state(false);

  $effect(() => {
    if (!open) return;
    name = agent ? tabName(agent) : '';
    error = '';
    confirmingStop = false;
  });

  const currentName = $derived(agent ? tabName(agent) : '');
  const changed = $derived(name.trim() !== '' && name.trim() !== currentName);

  async function save() {
    if (!agent || !changed || busy) return;
    const value = name.trim();
    busy = true;
    error = '';
    try {
      await relayStore.sendToAgent(agent, { type: 'agent_rename', name: value });
      relayStore.showToast(`Renamed to ${value}.`);
      open = false;
    } catch (failure) {
      error = failure instanceof Error ? failure.message : 'The agent could not be renamed.';
    } finally {
      busy = false;
    }
  }

  function openTerminal() {
    if (!agent) return;
    open = false;
    navigate({ view: 'terminal', paneId: agent.pane_id });
  }

  function openChat() {
    if (!agent) return;
    open = false;
    navigate({ view: 'history', paneId: agent.pane_id });
  }

  async function stop() {
    if (!agent || busy) return;
    if (!confirmingStop) {
      confirmingStop = true;
      return;
    }
    busy = true;
    error = '';
    try {
      await relayStore.sendToAgent(agent, { type: 'agent_stop' });
      relayStore.showToast('Agent stopped.');
      open = false;
      replaceView({ view: 'agents' });
    } catch (failure) {
      error = failure instanceof Error ? failure.message : 'The agent could not be stopped.';
    } finally {
      busy = false;
    }
  }
</script>

<AppDialog id="agent-sheet" bind:open title="Agent" description={agent ? `On ${hostLabel(agent)}` : ''}>
  {#if agent}
    <form class="form-stack" onsubmit={(event) => { event.preventDefault(); void save(); }}>
      <label class="field-label" for="agent-sheet-name">Name</label>
      <input id="agent-sheet-name" bind:value={name} autocomplete="off" placeholder={String(agent.agent || 'agent')} />
      <p class="hint">Renames the Herdr tab on your computer too.</p>

      <div class="sheet-rows" role="list">
        {#if agent.project}<div class="sheet-row" role="listitem"><span>Project</span><strong>{agent.project}</strong></div>{/if}
        <div class="sheet-row" role="listitem"><span>Agent</span><strong>{agent.agent || 'unknown'}</strong></div>
        {#if agent.herdr_session}<div class="sheet-row" role="listitem"><span>Session</span><strong>{agent.herdr_session}</strong></div>{/if}
        {#if agent.cwd}<div class="sheet-row" role="listitem"><span>Folder</span><strong class="sheet-path">{agent.cwd}</strong></div>{/if}
      </div>

      <div class="sheet-rows">
        <button type="button" class="sheet-action" onclick={openChat}>Open chat</button>
        <button type="button" class="sheet-action" onclick={openTerminal}>Open terminal</button>
        <button type="button" class="sheet-action danger" disabled={busy} onclick={() => { void stop(); }}>
          {confirmingStop ? 'Tap again to stop the agent' : 'Stop agent'}
        </button>
      </div>

      {#if error}<p class="board-notice error" role="alert">{error}</p>{/if}
      <div class="dialog-actions">
        <Button type="submit" disabled={!changed || busy}>Save</Button>
        <Button variant="ghost" disabled={busy} onclick={() => { open = false; }}>Cancel</Button>
      </div>
    </form>
  {/if}
</AppDialog>
