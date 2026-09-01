<script lang="ts">
  import Button from '$components/ui/Button.svelte';
  import {
    addComment,
    cancelRun,
    finishRun,
    getBoard,
    getCard,
    moveCard,
    retryCard,
  } from '$lib/board/client';
  import { cardStatusLabel, cardStatusTone, parseBoardTime } from '$lib/board/format';
  import { boardSelection } from '$lib/board/selection';
  import type { BoardCard, BoardColumn, BoardComment } from '$lib/board/types';
  import { closeCurrentView } from '$lib/router';
  import { relayStore } from '$lib/store';

  let { cardId }: { cardId: number } = $props();

  const boardRevision = relayStore.boardRevision;

  let card = $state<BoardCard | null>(null);
  let comments = $state<BoardComment[]>([]);
  let columns = $state<BoardColumn[]>([]);
  let comment = $state('');
  let error = $state('');
  let busy = $state(false);
  let loading = $state(true);
  let loadedKey = '';

  const relayId = $derived($boardSelection.relayId);
  const column = $derived(columns.find((entry) => entry.id === card?.column_id));
  // An open run is the daemon's own definition: these four states all mean a
  // run owns the card, so editing and deleting are refused until it ends.
  const openRun = $derived(['queued', 'running', 'blocked', 'awaiting'].includes(String(card?.status)));

  $effect(() => {
    if (!relayId || !cardId) return;
    const revision = $boardRevision.get(relayId) || 0;
    const key = `${relayId}:${cardId}:${revision}`;
    if (key === loadedKey) return;
    loadedKey = key;
    void load();
  });

  async function load() {
    loading = true;
    try {
      const detail = await getCard(relayId, cardId);
      card = detail.card;
      comments = detail.comments || [];
      error = '';
      if (card) {
        const snapshot = await getBoard(relayId, card.board_id);
        columns = [...(snapshot.columns || [])].sort((a, b) => a.position - b.position);
      }
    } catch (loadError) {
      error = loadError instanceof Error ? loadError.message : 'This card could not be read';
    } finally {
      loading = false;
    }
  }

  async function run(action: () => Promise<unknown>) {
    if (busy) return;
    busy = true;
    error = '';
    try {
      await action();
      loadedKey = '';
      await load();
    } catch (actionError) {
      error = actionError instanceof Error ? actionError.message : 'The board refused that action';
    } finally {
      busy = false;
    }
  }

  async function postComment() {
    const body = comment.trim();
    if (!body) return;
    await run(async () => {
      await addComment(relayId, cardId, body);
      comment = '';
    });
  }

  function commentTime(value: string): string {
    const at = parseBoardTime(value);
    return at ? new Date(at).toLocaleString() : '';
  }
</script>

<main class="page card-view" aria-label={card ? `Card ${card.id}` : 'Card'}>
  {#if loading && !card}
    <p role="status">Loading card…</p>
  {:else if !card}
    <p role="alert">{error || 'This card is not available.'}</p>
    <Button onclick={closeCurrentView}>Back to the board</Button>
  {:else}
    <header class="card-header">
      <h2>{card.title}</h2>
      <div class="card-chips">
        <span class={`status-dot status-${cardStatusTone(card.status)}`} aria-hidden="true"></span>
        <span class="card-status">{cardStatusLabel(card.status)}</span>
        {#if column}<span class="project-chip">{column.name}</span>{/if}
        {#if card.harness}<span class="project-chip">{card.harness}</span>{/if}
      </div>
    </header>

    {#if error}<p class="board-notice error" role="alert">{error}</p>{/if}

    <section class="card-actions" aria-label="Run">
      {#if openRun}
        <Button variant="danger" disabled={busy} onclick={() => run(() => cancelRun(relayId, cardId))}>Cancel run</Button>
      {/if}
      {#if card.status === 'awaiting'}
        <Button disabled={busy} onclick={() => run(() => finishRun(relayId, cardId, 'ok'))}>Mark done</Button>
      {/if}
      {#if card.status === 'failed' || card.status === 'done'}
        <Button variant="secondary" disabled={busy} onclick={() => run(() => retryCard(relayId, cardId))}>Retry</Button>
      {/if}
    </section>

    {#if columns.length}
      <section class="card-section" aria-label="Move">
        <h3>Move to</h3>
        <div class="card-columns">
          {#each columns as entry (entry.id)}
            <Button
              size="sm"
              variant={entry.id === card.column_id ? 'default' : 'secondary'}
              disabled={busy || openRun || entry.id === card.column_id}
              title={entry.trigger === 'auto' ? 'This column starts an agent' : undefined}
              onclick={() => run(() => moveCard(relayId, cardId, entry.id))}
            >{entry.name}{entry.trigger === 'auto' ? ' ▸' : ''}</Button>
          {/each}
        </div>
        {#if openRun}
          <p class="card-hint">A run owns this card. Cancel it before moving the card.</p>
        {/if}
      </section>
    {/if}

    {#if card.description}
      <section class="card-section" aria-label="Instruction">
        <h3>Instruction</h3>
        <p class="card-description">{card.description}</p>
      </section>
    {/if}

    <section class="card-section" aria-label="Comments">
      <h3>Comments{comments.length ? ` · ${comments.length}` : ''}</h3>
      {#each comments as entry (entry.id)}
        <article class="card-comment">
          <div class="card-comment-head">{entry.author || 'you'} · {commentTime(entry.created_at)}</div>
          <p>{entry.body}</p>
        </article>
      {/each}
      <form class="form-stack" onsubmit={(event) => { event.preventDefault(); void postComment(); }}>
        <label class="field-label" for="card-comment">Add a comment</label>
        <textarea id="card-comment" bind:value={comment} placeholder="Comments join the next run's prompt…"></textarea>
        <div class="dialog-actions">
          <Button type="submit" disabled={!comment.trim() || busy}>Comment</Button>
        </div>
      </form>
    </section>
  {/if}
</main>
