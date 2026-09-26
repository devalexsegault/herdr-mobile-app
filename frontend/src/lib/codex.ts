import { stripAnsi } from '$lib/terminal';

export interface CodexChoice {
  label: string;
  description: string;
  current: boolean;
}
export interface CodexChoices {
  kind: 'model' | 'effort' | 'mode' | 'permissions' | 'question';
  title: string;
  choices: CodexChoice[];
  focus: number;
  signature: string;
}

/** Only live numbered menus with an explicit submit footer are actionable. */
export function codexChoices(text: string): CodexChoices | null {
  const lines = stripAnsi(text).trimEnd().split(/\r?\n/).slice(-100);
  if (!/enter\s+(?:to\s+)?(?:select|confirm|submit)/i.test(lines.slice(-4).join(' '))) return null;
  let start = -1;
  let kind: CodexChoices['kind'] = 'question';
  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i].trim();
    let next: CodexChoices['kind'] | undefined;
    if (/^(?:select|choose) (?:a )?model\b/i.test(line)) next = 'model';
    else if (/^(?:select|choose) (?:a )?(?:reasoning|effort)\b/i.test(line)) next = 'effort';
    else if (/^(?:select|choose|change) (?:collaboration )?mode\b/i.test(line)) next = 'mode';
    else if (/^(?:select|choose|change) (?:approval |sandbox )?permissions?\b/i.test(line)) next = 'permissions';
    else if (/^question\s+\d+\s*\/\s*\d+/i.test(line)) next = 'question';
    if (next) { start = i; kind = next; }
  }
  if (start < 0) return null;
  const choices: CodexChoice[] = [];
  let focus = -1;
  const question: string[] = [];
  for (const line of lines.slice(start + 1)) {
    if (/enter\s+(?:to\s+)?(?:select|confirm|submit)/i.test(line)) break;
    const match = /^\s*([›❯>])?\s*(\d+)\.\s+(.+)$/.exec(line);
    if (!match) {
      if (!choices.length && line.trim()) question.push(line.trim());
      else if (choices.length && line.trim()) choices[choices.length - 1].description += ` ${line.trim()}`;
      continue;
    }
    if (Number(match[2]) !== choices.length + 1) return null;
    if (match[1]) focus = choices.length;
    const [label, ...description] = match[3].trim().split(/\s{2,}/);
    choices.push({ label, description: description.join(' '), current: /\(current\)/i.test(label) });
  }
  if (!choices.length || choices.length > 40 || focus < 0) return null;
  return { kind, title: kind === 'question' ? question.join(' ') || lines[start].trim() : lines[start].trim(), choices, focus, signature: lines.slice(start).join('\n') };
}

export function codexChoiceKeys(menu: CodexChoices, index: number): string[] {
  if (!Number.isInteger(index) || index < 0 || index >= menu.choices.length) return [];
  const distance = index - menu.focus;
  return [...Array<string>(Math.abs(distance)).fill(distance < 0 ? 'Up' : 'Down'), 'Enter'];
}
