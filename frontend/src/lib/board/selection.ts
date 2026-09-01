import { writable } from 'svelte/store';

/**
 * Which relay's board the phone is looking at. A board belongs to the machine
 * that runs its daemon, so this pair -- relay plus board -- is the whole
 * context, and it is shared between the board and a card opened from it.
 */
export interface BoardSelection {
  relayId: string;
  boardId: number;
}

const KEY = 'herdr-board-selection';

function read(): BoardSelection {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return { relayId: '', boardId: 0 };
    const parsed = JSON.parse(raw) as { relayId?: unknown; boardId?: unknown };
    return { relayId: String(parsed.relayId || ''), boardId: Number(parsed.boardId) || 0 };
  } catch {
    return { relayId: '', boardId: 0 };
  }
}

export const boardSelection = writable<BoardSelection>(read());

export function selectBoardContext(next: BoardSelection): void {
  boardSelection.set(next);
  try {
    localStorage.setItem(KEY, JSON.stringify(next));
  } catch {
    // A private window keeps the choice for this session only.
  }
}
