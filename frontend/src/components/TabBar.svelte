<script lang="ts">
  import { currentView, navigate, tabForView, tabRoot, type TabId } from '$lib/router';

  let {
    attention = 0,
    settingsLabel = 'Settings',
    updateBadge = false,
  }: { attention?: number; settingsLabel?: string; updateBadge?: boolean } = $props();

  const active = $derived(tabForView($currentView));

  const tabs: { id: TabId; label: string }[] = [
    { id: 'today', label: 'Today' },
    { id: 'agents', label: 'Agents' },
    { id: 'board', label: 'Board' },
    { id: 'activity', label: 'Activity' },
    { id: 'settings', label: 'Settings' },
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
      aria-label={tab.id === 'settings' ? settingsLabel : undefined}
      onclick={() => open(tab.id)}
    >
      <span class="tab-bar-icon">
        {#if tab.id === 'today'}
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
            <path d="M3 12 12 4l9 8"></path><path d="M5 10v10h14V10"></path>
          </svg>
        {:else if tab.id === 'agents'}
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
            <circle cx="9" cy="8" r="3.2"></circle><path d="M3.5 19a5.5 5.5 0 0 1 11 0"></path><circle cx="17" cy="9" r="2.4"></circle><path d="M15 19a4.5 4.5 0 0 1 5.5-4.3"></path>
          </svg>
        {:else if tab.id === 'board'}
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linejoin="round" aria-hidden="true" focusable="false">
            <rect x="3" y="4" width="5" height="16" rx="1.2"></rect><rect x="9.5" y="4" width="5" height="11" rx="1.2"></rect><rect x="16" y="4" width="5" height="7" rx="1.2"></rect>
          </svg>
        {:else if tab.id === 'activity'}
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
            <path d="M3 12h4l3-7 4 14 3-7h4"></path>
          </svg>
        {:else}
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
            <circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"></path>
          </svg>
        {/if}
        {#if tab.id === 'today' && attention > 0}
          <span class="tab-bar-badge" aria-hidden="true">{attention > 9 ? '9+' : attention}</span>
        {/if}
        {#if tab.id === 'settings' && updateBadge}
          <span class="tab-bar-update" aria-hidden="true"></span>
        {/if}
      </span>
      <span class="tab-bar-label">{tab.label}</span>
    </button>
  {/each}
</nav>
