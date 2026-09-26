import { render, screen, within } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import TerminalView from '$components/TerminalView.svelte';
import ConversationHistory from '$components/ConversationHistory.svelte';
import { relayStore } from '$lib/store';
import type { Agent } from '$lib/types';

const agent: Agent = {
  relay_id: 'test', relay_label: 'Test', raw_pane_id: 'p1', pane_id: 'test::p1',
  agent: 'codex', status: 'idle', project: 'chat',
};

function setup(content = '') {
  vi.spyOn(relayStore, 'readPane').mockImplementation(() => undefined);
  vi.spyOn(relayStore, 'getConversationHistory').mockResolvedValue({
    available: true, entries: [{ id: 'a', timestamp: '', role: 'assistant', text: 'Ready', model: 'codex-test', effort: 'high', mode: 'default' }],
    hasMore: false, total: 1, fileTruncated: false, reason: '',
  });
  relayStore.terminalFrames.set(new Map([[agent.pane_id, { paneId: agent.pane_id, content, format: 'text' }]]));
  return vi.spyOn(relayStore, 'sendToAgent').mockResolvedValue({ type: 'command_result', request_id: '1', ok: true });
}

afterEach(() => { vi.restoreAllMocks(); relayStore.terminalFrames.set(new Map()); });

describe('Codex chat controls', () => {
  it('opens the native model and reasoning picker from chat', async () => {
    const send = setup();
    const user = userEvent.setup();
    render(ConversationHistory, { agent });
    await user.click(await screen.findByRole('button', { name: 'Model: codex-test. Change model' }));
    expect(send).toHaveBeenCalledWith(agent, { type: 'submit_prompt', text: '/model' });
  });

  it('navigates a picker without sending chat text and reports failures', async () => {
    const send = setup('Select model\n› 1. codex-test\n  2. another-model\nPress enter to select or esc to go back');
    const user = userEvent.setup();
    render(ConversationHistory, { agent });
    const menu = screen.getByRole('region', { name: 'Codex menu' });
    expect(screen.getByRole('textbox', { name: 'Prompt' })).toBeDisabled();
    await user.click(within(menu).getByRole('button', { name: 'Down' }));
    expect(send).toHaveBeenCalledWith(agent, { type: 'send_keys', keys: ['Down'], activity_label: 'Down' });
    send.mockRejectedValueOnce(new Error('Relay disconnected'));
    await user.click(within(menu).getByRole('button', { name: 'Select' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Relay disconnected');
  });

  it('keeps verified approvals ahead of generic menu controls', async () => {
    setup('Approve command\nEnter to confirm · Esc to cancel');
    render(ConversationHistory, { agent: { ...agent, status: 'blocked', attention_kind: 'approval', attention_capable: true,
      command: 'Run tests?', options: ['Approve once', 'Deny'] } });
    expect(screen.queryByRole('region', { name: 'Codex menu' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Deny/ })).toBeInTheDocument();
  });
});


describe('Codex pending questions', () => {
  it('shows live numbered choices when classification has not supplied a form', async () => {
    const send = setup('Question 1/1\nWhich scope?\n› 1. This repository\n  2. None of the above\ntab to add notes | enter to submit answer | esc to interrupt');
    const user = userEvent.setup();
    render(ConversationHistory, { agent: { ...agent, status: 'blocked', attention_kind: 'unknown', attention_capable: true } });
    await user.click(screen.getByRole('button', { name: 'This repository' }));
    expect(send).toHaveBeenCalledWith(expect.objectContaining({ pane_id: agent.pane_id }), expect.objectContaining({ type: 'send_keys', keys: ['Enter'] }));
    expect(screen.queryByText('Switch to Terminal to handle the pending agent interaction.')).not.toBeInTheDocument();
  });

  it('allows an answer in the native free-text question editor', async () => {
    const send = setup('Add details\n›\nEnter submit · Esc cancel');
    const user = userEvent.setup();
    render(ConversationHistory, { agent: { ...agent, status: 'blocked', attention_kind: 'unknown', attention_capable: true } });
    const input = screen.getByRole('textbox', { name: 'Prompt' });
    expect(input).toBeEnabled();
    await user.type(input, 'Only this repo');
    await user.click(screen.getByRole('button', { name: 'Send prompt' }));
    expect(send).toHaveBeenNthCalledWith(1, expect.objectContaining({ pane_id: agent.pane_id }), { type: 'send_text', text: 'Only this repo' });
    expect(send).toHaveBeenNthCalledWith(2, expect.objectContaining({ pane_id: agent.pane_id }), expect.objectContaining({ type: 'send_keys', keys: ['Enter'] }));
  });
});


it('allows question notes to receive focus while terminal modifier controls are hidden', async () => {
  setup();
  const pending: Agent = { ...agent, status: 'blocked', attention_kind: 'question', attention_capable: true,
    interaction: { id: 'q1', kind: 'single_select', question: 'Which scope?', options: [{ index: 0, label: 'Repository' }], other: { label: 'Other', placeholder: 'Other answer' } } };
  render(TerminalView, { agent: pending, allAgents: [pending], responding: new Set<string>() });
  const input = screen.getByRole('textbox', { name: 'Other answer' });
  await userEvent.setup().click(input);
  expect(input).toHaveFocus();
});
