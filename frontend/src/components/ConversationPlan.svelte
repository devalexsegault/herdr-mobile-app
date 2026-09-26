<script lang="ts">
  import type { ConversationPlan } from '$lib/conversation';
  let { plan }: { plan: ConversationPlan } = $props();
  const labels = { pending: 'Pending', in_progress: 'In progress', completed: 'Completed' };
</script>

<section aria-label="Plan" class="conversation-plan">
  <h3>Plan</h3>
  {#if plan.explanation}<p>{plan.explanation}</p>{/if}
  <ol>
    {#each plan.steps as step, index (index)}
      <li aria-current={step.status === 'in_progress' ? 'step' : undefined}>
        <span>{step.step}</span> <small>{labels[step.status]}</small>
      </li>
    {/each}
  </ol>
</section>

<style>
  .conversation-plan { padding: .75rem; }
  li { margin-block: .5rem; overflow-wrap: anywhere; }
  small { opacity: .7; }
</style>
