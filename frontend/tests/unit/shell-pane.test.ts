import { render, screen } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import HomeView from '$components/HomeView.svelte';
import TerminalView from '$components/TerminalView.svelte';
import { isShellPane } from '$lib/agents';
import { relayStore } from '$lib/store';
import type { Agent } from '$lib/types';

const shell: Agent = {
  relay_id: 'fedora', relay_label: 'Fedora', raw_pane_id: 'w1:p2', pane_id: 'fedora::w1:p2',
  project: 'relay', cwd: '/src/relay', tab_label: 'build', status: '', shell: true,
};

afterEach(() => {
  vi.restoreAllMocks();
});

describe('shell panes', () => {
  it('recognizes a pane without an agent as a shell', () => {
    expect(isShellPane(shell)).toBe(true);
    expect(isShellPane({ ...shell, agent: 'codex' })).toBe(false);
  });

  it('lists shells on the home screen and opens them', async () => {
    const user = userEvent.setup();
    const onopen = vi.fn();
    render(HomeView, {
      agents: [], shells: [shell], relays: [], responding: new Set<string>(), onopen, onbrowse: vi.fn(),
    });
    expect(screen.getByRole('heading', { name: 'Terminals' })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Open terminal build on Fedora' }));
    expect(onopen).toHaveBeenCalledWith(shell);
  });

  it('runs a typed command in the shell without slash suggestions', async () => {
    const user = userEvent.setup();
    const send = vi.spyOn(relayStore, 'sendToAgent').mockResolvedValue({
      type: 'command_result', request_id: 'text-1', ok: true,
    });
    render(TerminalView, {
      agent: shell,
      allAgents: [],
      frame: { paneId: shell.pane_id, content: 'alexandre@host:~/src/relay$ ', format: 'plain' },
      responding: new Set<string>(),
    });
    const input = screen.getByRole('combobox', { name: 'Command' });
    expect(input).toBeEnabled();
    expect(input).toHaveAttribute('autocapitalize', 'none');
    await user.type(input, '/usr/bin/ls -la');
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Run command' }));
    expect(send).toHaveBeenNthCalledWith(1, shell, { type: 'send_text', text: '/usr/bin/ls -la' });
    expect(send).toHaveBeenNthCalledWith(2, shell, {
      type: 'send_keys', keys: ['Enter'], activity_label: 'Ran command',
    });
  });
});
