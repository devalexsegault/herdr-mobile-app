import { render, screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import ConversationHistory from '$components/ConversationHistory.svelte';
import { relayStore } from '$lib/store';
import type { Agent } from '$lib/types';

const agent: Agent = {
  relay_id: 'fedora', relay_label: 'Fedora', raw_pane_id: 'w1:p9', pane_id: 'fedora::w1:p9',
  project: 'relay', agent: 'claude', status: 'working', cwd: '/home/test/relay',
};

afterEach(() => {
  vi.restoreAllMocks();
});

describe('working indicator', () => {
  it('shows that the agent is working at the end of the chat', async () => {
    vi.spyOn(relayStore, 'getConversationHistory').mockResolvedValue({
      available: true, reason: '', entries: [], hasMore: false, total: 0, fileTruncated: false,
    });
    const view = render(ConversationHistory, { agent });
    expect(await screen.findByText('claude is working…')).toBeInTheDocument();

    await view.rerender({ agent: { ...agent, status: 'idle' } });
    expect(screen.queryByText('claude is working…')).not.toBeInTheDocument();
  });
});
