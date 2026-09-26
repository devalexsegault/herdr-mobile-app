import { describe, expect, it } from 'vitest';
import { codexChoices, codexChoiceKeys } from '$lib/codex';

describe('Codex live settings menus', () => {
  it('reads models and descriptions and moves relative to the visible cursor', () => {
    const menu = codexChoices('Select Model\n  1. model-a  Fast\n› 2. model-b (current)  Thorough\nPress enter to select or esc to go back');
    expect(menu?.kind).toBe('model');
    expect(menu?.choices[1]).toMatchObject({ current: true, description: 'Thorough' });
    expect(codexChoiceKeys(menu!, 0)).toEqual(['Up', 'Enter']);
    expect(codexChoiceKeys(menu!, 8)).toEqual([]);
  });
  it('distinguishes reasoning levels from model selection', () => {
    const menu = codexChoices('Select Reasoning Level for model-a\n› 1. Low  Quick\n  2. High  Thorough\nEnter to select · Esc to cancel');
    expect(menu?.kind).toBe('effort');
    expect(codexChoiceKeys(menu!, 1)).toEqual(['Down', 'Enter']);
  });
  it('does not turn transcript prose or an old menu into actionable choices', () => {
    expect(codexChoices('Here are the models\n› 1. model-a\nEnter to select')).toBeNull();
    expect(codexChoices('Select Model\n› 1. model-a\nEnter to select\n\nDone\nReady\n›')).toBeNull();
    expect(codexChoices('Select Model\n  1. model-a\nEnter to select')).toBeNull();
  });
});
