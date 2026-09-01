<script lang="ts">
  import { currentView, navigate, tabForView, tabRoot, type TabId } from '$lib/router';

  let { attention = 0 }: { attention?: number } = $props();

  const active = $derived(tabForView($currentView));

  const tabs: { id: TabId; label: string }[] = [
    { id: 'today', label: 'Today' },
    { id: 'board', label: 'Board' },
    { id: 'activity', label: 'Activity' },
  ];

  function open(tab: TabId) {
    if (active === tab) return;
    navigate(tabRoot(tab));
  }
</script>

<nav class="tab-bar" aria-label="Sections">
  {#each tabs as tab (tab.id)}
    <button
      class="tab-bar-item"
      class:active={active === tab.id}
      aria-current={active === tab.id ? 'page' : undefined}
      onclick={() => open(tab.id)}
    >
      <span class="tab-bar-icon">
        {#if tab.id === 'today'}
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
            <path d="M3 13h4l2 4 4-10 2 6h6"></path>
          </svg>
        {:else if tab.id === 'board'}
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" aria-hidden="true" focusable="false">
            <rect x="3" y="4" width="5" height="16" rx="1.5"></rect>
            <rect x="9.5" y="4" width="5" height="11" rx="1.5"></rect>
            <rect x="16" y="4" width="5" height="14" rx="1.5"></rect>
          </svg>
        {:else}
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
            <circle cx="12" cy="12" r="8.5"></circle>
            <path d="M12 7.5V12l3 2"></path>
          </svg>
        {/if}
        {#if tab.id === 'today' && attention > 0}
          <span class="tab-bar-badge" aria-hidden="true">{attention > 9 ? '9+' : attention}</span>
        {/if}
      </span>
      <span class="tab-bar-label">{tab.label}</span>
    </button>
  {/each}
</nav>
