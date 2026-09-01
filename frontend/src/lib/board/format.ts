import type { CardStatus } from './types';

/**
 * Card colour follows the same rule as agents: one colour, one meaning.
 * Running takes the accent, done takes green, anything the person has to look
 * at takes amber or red.
 */
export function cardStatusTone(status: CardStatus | string): 'danger' | 'warning' | 'primary' | 'success' | 'muted' {
  if (status === 'failed' || status === 'blocked') return 'danger';
  if (status === 'awaiting') return 'warning';
  if (status === 'running') return 'primary';
  if (status === 'queued') return 'warning';
  if (status === 'done') return 'success';
  return 'muted';
}

export function cardStatusLabel(status: CardStatus | string): string {
  switch (status) {
    case 'running': return 'running';
    case 'queued': return 'queued';
    case 'blocked': return 'blocked';
    case 'awaiting': return 'needs review';
    case 'done': return 'done';
    case 'failed': return 'failed';
    case 'idle': return 'idle';
    default: return String(status || 'unknown');
  }
}

/**
 * boardd timestamps are naive UTC ("2026-09-01 19:47:01"), so they are read as
 * UTC rather than as local time: parsing them as local drifts the age of every
 * run by the viewer's offset.
 */
export function parseBoardTime(value: string): number {
  if (!value) return 0;
  const iso = value.includes('T') ? value : `${value.replace(' ', 'T')}Z`;
  const parsed = Date.parse(iso);
  return Number.isFinite(parsed) ? parsed : 0;
}

export function runAge(startedAt: string, now: number): string {
  const started = parseBoardTime(startedAt);
  if (!started) return '';
  const seconds = Math.max(0, Math.round((now - started) / 1000));
  if (seconds < 60) return 'just now';
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours}h`;
  return `${Math.round(hours / 24)}d`;
}
