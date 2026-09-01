<script lang="ts">
  import AppDialog from '$components/ui/AppDialog.svelte';
  import Button from '$components/ui/Button.svelte';
  import { createCard, listHarnesses, listSpaces } from '$lib/board/client';
  import type { BoardColumn, BoardSpace } from '$lib/board/types';

  let {
    open = $bindable(false),
    relayId,
    boardId,
    columnId,
    columns,
    oncreated,
  }: {
    open?: boolean;
    relayId: string;
    boardId: number;
    columnId: number;
    columns: BoardColumn[];
    oncreated: () => void;
  } = $props();

  let title = $state('');
  let description = $state('');
  let harness = $state('');
  let space = $state('');
  let harnesses = $state<string[]>([]);
  let spaces = $state<BoardSpace[]>([]);
  let busy = $state(false);
  let error = $state('');
  let loadedFor = '';

  const column = $derived(columns.find((entry) => entry.id === columnId));
  // Naming the consequence is the whole point: an auto column turns "create"
  // into "create and dispatch a real agent".
  const dispatches = $derived(column?.trigger === 'auto');

  $effect(() => {
    if (!open || !relayId || loadedFor === relayId) return;
    loadedFor = relayId;
    void loadOptions();
  });

  $effect(() => {
    if (open) return;
    title = '';
    description = '';
    error = '';
  });

  async function loadOptions() {
    try {
      const [harnessList, spaceList] = await Promise.all([
        listHarnesses(relayId),
        listSpaces(relayId),
      ]);
      harnesses = harnessList.harnesses || [];
      spaces = spaceList.spaces || [];
      if (!harness) harness = harnesses[0] || '';
    } catch {
      // The pickers are optional; the daemon applies its own defaults when the
      // card omits them.
    }
  }

  async function submit() {
    const trimmed = title.trim();
    if (!trimmed || busy) return;
    busy = true;
    error = '';
    try {
      await createCard(relayId, {
        title: trimmed,
        board_id: boardId,
        description: description.trim() || undefined,
        column_id: columnId || undefined,
        harness: harness || undefined,
        space_kind: space ? 'workspace' : undefined,
        space_ref: space || undefined,
      });
      open = false;
      oncreated();
    } catch (createError) {
      error = createError instanceof Error ? createError.message : 'The card could not be created';
    } finally {
      busy = false;
    }
  }
</script>

<AppDialog
  id="board-card-compose"
  bind:open
  title="New card"
  description={column ? `Into ${column.name}` : 'Into the board'}
>
  <form
    class="form-stack"
    onsubmit={(event) => { event.preventDefault(); void submit(); }}
  >
    <label class="field-label" for="card-title">Title</label>
    <input id="card-title" bind:value={title} required autocomplete="off" />

    <label class="field-label" for="card-description">Instruction</label>
    <textarea id="card-description" bind:value={description} placeholder="What the agent should do…"></textarea>

    {#if harnesses.length}
      <label class="field-label" for="card-harness">Agent</label>
      <select id="card-harness" bind:value={harness}>
        {#each harnesses as name (name)}<option value={name}>{name}</option>{/each}
      </select>
    {/if}

    {#if spaces.length}
      <label class="field-label" for="card-space">Space</label>
      <select id="card-space" bind:value={space}>
        <option value="">Board default</option>
        {#each spaces as entry (entry.id)}<option value={entry.id}>{entry.label}</option>{/each}
      </select>
    {/if}

    {#if dispatches}
      <p class="board-notice" role="status">This column starts the agent as soon as the card is created.</p>
    {/if}
    {#if error}<p class="board-notice error" role="alert">{error}</p>{/if}

    <div class="dialog-actions">
      <Button type="submit" disabled={!title.trim() || busy}>{dispatches ? 'Create and start' : 'Create card'}</Button>
      <Button variant="ghost" disabled={busy} onclick={() => { open = false; }}>Cancel</Button>
    </div>
  </form>
</AppDialog>
