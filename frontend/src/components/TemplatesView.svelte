<script lang="ts">
  import TemplateEditor from '$components/TemplateEditor.svelte';
  import AppDialog from '$components/ui/AppDialog.svelte';
  import Button from '$components/ui/Button.svelte';
  import { agentStatusGroup } from '$lib/agents';
  import { applyTemplate, deleteTemplate, getBoard, listTemplates, saveTemplate } from '$lib/board/client';
  import { blankTemplate, designTemplateWithAI, duplicateName } from '$lib/board/templates';
  import type { BoardTemplate, BoardTemplateApplyResult, ProjectEntry } from '$lib/board/types';
  import { relayStore } from '$lib/store';

  let {
    relayId,
    projects,
    boardId,
  }: {
    relayId: string;
    projects: ProjectEntry[];
    /** The board currently open, offered first when applying. */
    boardId: number;
  } = $props();

  const connections = relayStore.connections;
  const agents = relayStore.agents;

  let templates = $state<BoardTemplate[]>([]);
  let dir = $state('');
  let loading = $state(true);
  let error = $state('');
  let notice = $state('');
  let busy = $state(false);

  let editorOpen = $state(false);
  let editing = $state<BoardTemplate>(blankTemplate());

  let applyOpen = $state(false);
  let applying = $state<BoardTemplate | null>(null);
  let applyBoardId = $state(0);
  let applyMode = $state<'replace' | 'append'>('append');
  let applyDeletes = $state<string[]>([]);
  let applyResult = $state<BoardTemplateApplyResult | null>(null);

  let designOpen = $state(false);
  let designName = $state('');
  let designIntent = $state('');

  let deleting = $state<BoardTemplate | null>(null);

  const profiles = $derived($connections.get(relayId)?.agentProfiles || []);
  let loadedFor = '';

  $effect(() => {
    if (!relayId || loadedFor === relayId) return;
    loadedFor = relayId;
    void load();
  });

  // A designer finishing its turn in the templates directory means a file may
  // have changed; reload rather than asking the person to.
  let designerSignature = '';
  $effect(() => {
    if (!dir) return;
    const signature = $agents
      .filter((agent) => agent.relay_id === relayId && agent.cwd === dir)
      .map((agent) => `${agent.pane_id}:${agentStatusGroup(agent)}`)
      .join(',');
    if (signature === designerSignature) return;
    const finished = signature.includes(':done') || signature.includes(':ready');
    designerSignature = signature;
    if (finished) void load();
  });

  async function load() {
    loading = true;
    error = '';
    try {
      const list = await listTemplates(relayId);
      templates = list.templates || [];
      dir = list.dir || '';
    } catch (loadError) {
      error = loadError instanceof Error ? loadError.message : 'Templates could not be read';
    } finally {
      loading = false;
    }
  }

  function openNew() {
    editing = blankTemplate();
    editorOpen = true;
  }

  function openEdit(template: BoardTemplate) {
    editing = template;
    editorOpen = true;
  }

  async function duplicate(template: BoardTemplate) {
    if (busy) return;
    busy = true;
    error = '';
    try {
      await saveTemplate(relayId, { ...template, name: duplicateName(template, templates) });
      await load();
    } catch (failure) {
      error = failure instanceof Error ? failure.message : 'The template could not be duplicated';
    } finally {
      busy = false;
    }
  }

  async function confirmDelete() {
    if (!deleting || busy) return;
    busy = true;
    error = '';
    try {
      await deleteTemplate(relayId, deleting.name);
      notice = `Deleted ${deleting.name}.`;
      deleting = null;
      await load();
    } catch (failure) {
      error = failure instanceof Error ? failure.message : 'The template could not be deleted';
    } finally {
      busy = false;
    }
  }

  function openApply(template: BoardTemplate) {
    applying = template;
    applyBoardId = boardId || projects[0]?.boards[0]?.id || 0;
    applyMode = 'append';
    applyDeletes = [];
    applyResult = null;
    applyOpen = true;
    void previewDeletes();
  }

  // Replace deletes the board's columns the template does not name; say
  // which before anything happens.
  async function previewDeletes() {
    applyDeletes = [];
    if (!applying || !applyBoardId || applyMode !== 'replace') return;
    try {
      const snapshot = await getBoard(relayId, applyBoardId);
      const kept = new Set(applying.columns.map((column) => column.name.toLowerCase()));
      applyDeletes = (snapshot.columns || [])
        .filter((column) => !kept.has(column.name.toLowerCase()))
        .map((column) => column.name);
    } catch {
      applyDeletes = [];
    }
  }

  async function confirmApply() {
    if (!applying || !applyBoardId || busy) return;
    busy = true;
    error = '';
    try {
      const applied = await applyTemplate(relayId, applyBoardId, applying.name, applyMode);
      applyResult = applied.result;
    } catch (failure) {
      error = failure instanceof Error ? failure.message : 'The template could not be applied';
    } finally {
      busy = false;
    }
  }

  function boardLabel(id: number): string {
    for (const entry of projects) {
      for (const board of entry.boards) {
        if (board.id === id) return entry.boards.length > 1 ? `${entry.project.name} · ${board.name}` : entry.project.name;
      }
    }
    return `Board ${id}`;
  }

  async function startDesign() {
    const name = designName.trim();
    if (!name || busy) return;
    busy = true;
    error = '';
    try {
      await designTemplateWithAI(relayId, name, designIntent.trim(), profiles);
      designOpen = false;
    } catch (failure) {
      error = failure instanceof Error ? failure.message : 'The designer could not be started';
    } finally {
      busy = false;
    }
  }

  function summary(template: BoardTemplate): string {
    const auto = template.columns.filter((column) => column.trigger === 'auto').length;
    return `${template.columns.length} columns · ${auto} start an agent`;
  }
</script>

<section class="templates-view" aria-label="Board templates">
  <div class="templates-actions">
    <Button size="sm" onclick={openNew}>New template</Button>
    <Button size="sm" variant="secondary" onclick={() => { designName = ''; designIntent = ''; designOpen = true; }}>Design with AI</Button>
  </div>
  {#if error}<p class="board-notice error" role="alert">{error}</p>{/if}
  {#if notice}<p class="board-notice" role="status">{notice}</p>{/if}

  {#if loading && !templates.length}
    <div class="empty-state" role="status">Loading templates…</div>
  {:else if !templates.length}
    <div class="empty-state">
      <h2>No template yet</h2>
      <p>Save a board as a template, build one here, or let an agent design it with you.</p>
    </div>
  {:else}
    <div class="template-list">
      {#each templates as template (template.name)}
        <article class="template-card" aria-label={template.name}>
          <header class="template-card-head">
            <h2>{template.name}</h2>
            <span class="template-card-meta">{summary(template)}</span>
          </header>
          {#if template.description}<p class="template-card-description">{template.description}</p>{/if}
          <div class="template-columns" aria-label="Columns">
            {#each template.columns as column (column.name)}
              <span class="project-chip" class:auto={column.trigger === 'auto'}>{column.name}</span>
            {/each}
          </div>
          <div class="template-card-actions">
            <Button size="sm" onclick={() => openApply(template)}>Apply…</Button>
            <Button size="sm" variant="secondary" onclick={() => openEdit(template)}>Edit</Button>
            <Button size="sm" variant="secondary" disabled={busy} onclick={() => { void duplicate(template); }}>Duplicate</Button>
            <Button size="sm" variant="ghost" onclick={() => { deleting = template; }}>Delete</Button>
          </div>
        </article>
      {/each}
    </div>
  {/if}
</section>

<TemplateEditor bind:open={editorOpen} {relayId} template={editing} onsaved={() => { void load(); }} />

<AppDialog id="board-template-apply" bind:open={applyOpen} title={applying ? `Apply ${applying.name}` : 'Apply template'} description="Columns are matched by name; the template's transitions are set on the columns it adds.">
  {#if applying}
    <div class="form-stack">
      {#if applyResult}
        <p class="board-notice" role="status">
          Applied to {boardLabel(applyBoardId)}: {applyResult.created.length} created, {applyResult.updated.length} updated, {applyResult.deleted.length} deleted.
        </p>
        <div class="dialog-actions">
          <Button onclick={() => { applyOpen = false; }}>Done</Button>
        </div>
      {:else}
        <label class="field-label" for="apply-board">Board</label>
        <select id="apply-board" bind:value={applyBoardId} onchange={() => { void previewDeletes(); }}>
          {#each projects as entry (entry.project.id)}
            {#each entry.boards as board (board.id)}
              <option value={board.id}>{boardLabel(board.id)}</option>
            {/each}
          {/each}
        </select>
        <div class="choice-grid settings-grid" role="group" aria-label="Mode">
          <button type="button" class="choice" class:active={applyMode === 'append'} aria-pressed={applyMode === 'append'} onclick={() => { applyMode = 'append'; applyDeletes = []; }}>
            <strong>Append</strong>
            <small>Adds the columns the board lacks; existing ones stay as they are.</small>
          </button>
          <button type="button" class="choice" class:active={applyMode === 'replace'} aria-pressed={applyMode === 'replace'} onclick={() => { applyMode = 'replace'; void previewDeletes(); }}>
            <strong>Replace</strong>
            <small>Rewrites matching columns, removes the others, orders like the template.</small>
          </button>
        </div>
        {#if applyMode === 'replace' && applyDeletes.length}
          <p class="board-notice error" role="status">
            Replace will delete {applyDeletes.join(', ')}. Their cards move to {applying.columns[0]?.name}.
          </p>
        {/if}
        <div class="dialog-actions">
          <Button variant={applyMode === 'replace' ? 'danger' : 'default'} disabled={busy || !applyBoardId} onclick={() => { void confirmApply(); }}>
            {applyMode === 'replace' ? 'Replace columns' : 'Add columns'}
          </Button>
          <Button variant="ghost" disabled={busy} onclick={() => { applyOpen = false; }}>Cancel</Button>
        </div>
      {/if}
    </div>
  {/if}
</AppDialog>

<AppDialog id="board-template-design" bind:open={designOpen} title="Design with AI" description="A Claude Code agent starts in the templates folder and writes the file with you; you talk to it in its conversation.">
  <form class="form-stack" onsubmit={(event) => { event.preventDefault(); void startDesign(); }}>
    <label class="field-label" for="design-name">Template name</label>
    <input id="design-name" bind:value={designName} required autocomplete="off" placeholder="Docs sprint" />
    <label class="field-label" for="design-intent">What it is for</label>
    <textarea id="design-intent" bind:value={designIntent} rows="3" placeholder="Write, review and publish documentation for a feature…"></textarea>
    {#if !profiles.length}<p class="hint">This relay has no agent profile; add one in Herdr first.</p>{/if}
    <div class="dialog-actions">
      <Button type="submit" disabled={busy || !designName.trim() || !profiles.length}>Start designing</Button>
      <Button variant="ghost" disabled={busy} onclick={() => { designOpen = false; }}>Cancel</Button>
    </div>
  </form>
</AppDialog>

<AppDialog id="board-template-delete" open={Boolean(deleting)} title="Delete template" description={deleting ? `${deleting.name} is removed from this computer. Boards built from it are not affected.` : ''}>
  <div class="dialog-actions">
    <Button variant="danger" disabled={busy} onclick={() => { void confirmDelete(); }}>Delete</Button>
    <Button variant="ghost" disabled={busy} onclick={() => { deleting = null; }}>Cancel</Button>
  </div>
</AppDialog>
