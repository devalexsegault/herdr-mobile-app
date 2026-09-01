<script lang="ts">
  import CardCompose from '$components/CardCompose.svelte';
  import AppDialog from '$components/ui/AppDialog.svelte';
  import Button from '$components/ui/Button.svelte';
  import { createProject, getBoard, listProjects, selectBoard } from '$lib/board/client';
  import { cardStatusTone, runAge } from '$lib/board/format';
  import { boardSelection, selectBoardContext } from '$lib/board/selection';
  import type { BoardCard, BoardSnapshot, ProjectEntry } from '$lib/board/types';
  import { navigate } from '$lib/router';
  import { relayStore } from '$lib/store';

  const connections = relayStore.connections;
  const boardRevision = relayStore.boardRevision;

  // A board belongs to one machine: the relay that runs its daemon. Boards are
  // never merged across relays, so the app picks one relay and stays there.
  const boardRelays = $derived([...$connections.values()].filter(
    (connection) => connection.status === 'connected' && connection.board,
  ));

  let relayId = $state('');
  let boardId = $state(0);
  let projects = $state<ProjectEntry[]>([]);
  let snapshot = $state<BoardSnapshot | null>(null);
  let error = $state('');
  let loading = $state(false);
  let switcherOpen = $state(false);
  let composeOpen = $state(false);
  let composeColumnId = $state(0);
  let newProjectPath = $state('');
  let creatingProject = $state(false);
  let now = $state(Date.now());
  let loadedKey = '';

  const activeConnection = $derived($connections.get(relayId));
  const columns = $derived([...(snapshot?.columns || [])].sort((a, b) => a.position - b.position));
  const runningCards = $derived(new Map(
    (snapshot?.active_runs || []).map((run) => [run.card_id, run.started_at]),
  ));

  $effect(() => {
    const timer = setInterval(() => { now = Date.now(); }, 30_000);
    return () => clearInterval(timer);
  });

  // Pick up the remembered relay and board, falling back to the first relay
  // that actually has a daemon.
  $effect(() => {
    if (relayId && boardRelays.some((connection) => connection.relay.id === relayId)) return;
    const stored = $boardSelection;
    const match = boardRelays.find((connection) => connection.relay.id === stored?.relayId);
    const chosen = match || boardRelays[0];
    if (!chosen) return;
    relayId = chosen.relay.id;
    if (match && stored?.boardId) boardId = stored.boardId;
  });

  $effect(() => {
    if (!relayId) return;
    const revision = $boardRevision.get(relayId) || 0;
    const key = `${relayId}:${boardId}:${revision}`;
    if (key === loadedKey) return;
    loadedKey = key;
    void load();
  });

  function rememberSelection() {
    selectBoardContext({ relayId, boardId });
  }

  async function load() {
    if (!relayId) return;
    loading = true;
    error = '';
    try {
      const list = await listProjects(relayId);
      projects = list.projects || [];
      if (!boardId) boardId = defaultBoardId(list);
      if (!boardId) {
        snapshot = null;
        return;
      }
      snapshot = await getBoard(relayId, boardId);
      rememberSelection();
    } catch (loadError) {
      error = loadError instanceof Error ? loadError.message : 'The board could not be read';
    } finally {
      loading = false;
    }
  }

  function defaultBoardId(list: { projects: ProjectEntry[]; selected_project_id?: number }): number {
    const selected = list.projects.find((entry) => entry.project.id === list.selected_project_id);
    const entry = selected || list.projects[0];
    if (!entry) return 0;
    return entry.selected_board_id || entry.boards[0]?.id || 0;
  }

  function cardsFor(columnId: number): BoardCard[] {
    return (snapshot?.cards || [])
      .filter((card) => card.column_id === columnId && !card.archived_at)
      .sort((a, b) => a.position - b.position);
  }

  async function chooseBoard(nextBoardId: number) {
    switcherOpen = false;
    if (nextBoardId === boardId) return;
    boardId = nextBoardId;
    snapshot = null;
    rememberSelection();
    try {
      await selectBoard(relayId, nextBoardId);
    } catch {
      // Persisting the daemon-side selection is a convenience; the app already
      // remembers its own choice.
    }
    await load();
  }

  async function addProject() {
    const path = newProjectPath.trim();
    if (!path || creatingProject) return;
    creatingProject = true;
    error = '';
    try {
      const created = await createProject(relayId, path);
      newProjectPath = '';
      switcherOpen = false;
      if (created?.board?.board?.id) {
        boardId = created.board.board.id;
        snapshot = null;
      }
      await load();
    } catch (createError) {
      error = createError instanceof Error ? createError.message : 'The project could not be created';
    } finally {
      creatingProject = false;
    }
  }

  function openCompose(columnId: number) {
    composeColumnId = columnId;
    composeOpen = true;
  }

  const boardName = $derived.by(() => {
    const project = projects.find((entry) => entry.project.id === snapshot?.board.project_id);
    if (!project) return snapshot?.board.name || 'Board';
    if (project.boards.length > 1) return `${project.project.name} · ${snapshot?.board.name}`;
    return project.project.name;
  });
</script>

<main class="board-view" aria-label="Board">
  {#if !boardRelays.length}
    <div class="empty-state">
      <h2>No board yet</h2>
      <p>Install the herdr-board plugin on the computer running the relay, then reconnect.</p>
    </div>
  {:else}
    <div class="board-bar">
      <button class="board-switch" onclick={() => { switcherOpen = true; }}>
        <span class="board-switch-name">{boardName}</span>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.1" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
          <path d="m6 9 6 6 6-6"></path>
        </svg>
      </button>
      {#if snapshot}
        <span class="board-bar-meta">{snapshot.cards.filter((card) => !card.archived_at).length} cards</span>
      {/if}
    </div>

    {#if activeConnection?.board && !activeConnection.board.herdr_connected}
      <p class="board-notice" role="status">
        The board daemon cannot reach Herdr on that computer, so cards can be read and edited but not dispatched.
      </p>
    {/if}
    {#if error}<p class="board-notice error" role="alert">{error}</p>{/if}

    {#if !snapshot && loading}
      <div class="empty-state" role="status">Loading board…</div>
    {:else if !snapshot}
      <div class="empty-state" role="status">This relay has no board to show yet.</div>
    {:else}
      <div class="board-columns">
        {#each columns as column (column.id)}
          {@const cards = cardsFor(column.id)}
          <section class="board-column" aria-label={column.name}>
            <header class="board-column-head">
              <h2>{column.name}</h2>
              <span class="board-column-count">{cards.length}</span>
              {#if column.trigger === 'auto'}
                <span class="board-column-auto">starts an agent</span>
              {/if}
            </header>
            <div class="board-column-cards">
              {#each cards as card (card.id)}
                <button class="board-card" onclick={() => navigate({ view: 'board_card', cardId: card.id })}>
                  <span class="board-card-head">
                    <span class={`status-dot status-${cardStatusTone(card.status)}`} aria-hidden="true"></span>
                    <span class="board-card-id">#{card.id}</span>
                    {#if card.harness}<span class="board-card-harness">{card.harness}</span>{/if}
                    {#if runningCards.has(card.id)}
                      <span class="board-card-age">{runAge(runningCards.get(card.id) || '', now)}</span>
                    {/if}
                  </span>
                  <span class="board-card-title">{card.title}</span>
                </button>
              {/each}
              <button class="board-card-add" onclick={() => openCompose(column.id)}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true" focusable="false">
                  <path d="M12 5v14M5 12h14"></path>
                </svg>
                Add a card
              </button>
            </div>
          </section>
        {/each}
      </div>
    {/if}
  {/if}
</main>

<AppDialog id="board-switcher" bind:open={switcherOpen} title="Boards" description="Pick a project, or add one from a folder on this computer.">
  <div class="form-stack">
    {#each projects as entry (entry.project.id)}
      <div class="board-project">
        <span class="board-project-name">{entry.project.name}</span>
        <div class="board-project-boards">
          {#each entry.boards as board (board.id)}
            <Button
              variant={board.id === boardId ? 'default' : 'secondary'}
              size="sm"
              onclick={() => { void chooseBoard(board.id); }}
            >{board.name}</Button>
          {/each}
        </div>
      </div>
    {/each}
    <label class="field-label" for="board-new-project">Add a project from a folder</label>
    <input
      id="board-new-project"
      bind:value={newProjectPath}
      placeholder="/home/you/code/project"
      autocomplete="off"
      spellcheck="false"
    />
    <div class="dialog-actions">
      <Button disabled={!newProjectPath.trim() || creatingProject} onclick={() => { void addProject(); }}>Create project</Button>
      <Button variant="ghost" onclick={() => { switcherOpen = false; }}>Close</Button>
    </div>
  </div>
</AppDialog>

<CardCompose
  bind:open={composeOpen}
  {relayId}
  {boardId}
  columnId={composeColumnId}
  columns={columns}
  oncreated={() => { void load(); }}
/>
