/**
 * Starting the agent that designs a template or edits a board.
 *
 * The relay writes the brief and names the working directory; the app starts
 * a Claude Code agent there through the ordinary launch flow and opens its
 * conversation, so the person talks to it like to any other agent.
 */
import { clientPaneId } from '$lib/agents';
import { replaceView } from '$lib/router';
import { relayStore } from '$lib/store';
import type { AgentProfile } from '$lib/types';
import { templateBrief } from './client';
import type { BoardTemplate, BoardTemplateBrief } from './types';

/** Claude Code is the default designer; any profile naming it wins. */
export function designerProfile(profiles: AgentProfile[]): AgentProfile | undefined {
  return profiles.find((profile) => /claude/i.test(`${profile.id} ${profile.label || ''}`))
    || profiles[0];
}

export async function launchTemplateAgent(
  relayId: string,
  brief: BoardTemplateBrief,
  profiles: AgentProfile[],
): Promise<void> {
  const profile = designerProfile(profiles);
  if (!profile) throw new Error('This relay has no agent profile to start the designer with');
  const result = await relayStore.sendCommand(relayId, {
    type: 'agent_start',
    profile_id: profile.id,
    name: brief.label,
    cwd: brief.cwd,
    prompt: brief.prompt,
    workspace_id: '',
    herdr_session: '',
  }, 45_000);
  const rawPaneId = String(result.data?.pane_id || '');
  const agent = await relayStore.waitForAgent(relayId, { rawPaneId, name: brief.label, cwd: brief.cwd });
  const paneId = agent?.pane_id || (rawPaneId ? clientPaneId(relayId, rawPaneId) : '');
  if (!paneId) throw new Error('The designer started but did not report its pane');
  replaceView({ view: 'history', paneId });
}

export async function designTemplateWithAI(
  relayId: string,
  name: string,
  intent: string,
  profiles: AgentProfile[],
): Promise<void> {
  const brief = await templateBrief(relayId, { kind: 'design', name, intent });
  await launchTemplateAgent(relayId, brief, profiles);
}

export async function editBoardWithAI(
  relayId: string,
  boardId: number,
  intent: string,
  profiles: AgentProfile[],
): Promise<void> {
  const brief = await templateBrief(relayId, { kind: 'edit', board_id: boardId, intent });
  await launchTemplateAgent(relayId, brief, profiles);
}

/** A copy that does not collide with the existing names. */
export function duplicateName(template: BoardTemplate, existing: BoardTemplate[]): string {
  const taken = new Set(existing.map((entry) => entry.name.toLowerCase()));
  let candidate = `${template.name} copy`;
  let index = 2;
  while (taken.has(candidate.toLowerCase())) {
    candidate = `${template.name} copy ${index}`;
    index += 1;
  }
  return candidate.slice(0, 64);
}

/** A blank template the editor starts from. */
export function blankTemplate(): BoardTemplate {
  return {
    name: '',
    description: '',
    columns: [
      { name: 'Backlog', trigger: 'manual' },
      { name: 'Execute', trigger: 'auto', system_prompt: '', on_success: 'Done', on_fail: 'Backlog' },
      { name: 'Done', trigger: 'manual' },
    ],
  };
}
