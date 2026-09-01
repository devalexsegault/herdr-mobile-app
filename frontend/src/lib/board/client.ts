/**
 * Typed calls onto the relay's board bridge.
 *
 * Every signature here comes from herdr-board's protocol v1 contract. The relay
 * forwards params untouched and validates only the method name, so this file is
 * the one place the app has to follow when the board protocol grows.
 */
import { relayStore } from '$lib/store';
import type {
  BoardCard,
  BoardColumn,
  BoardComment,
  BoardSession,
  BoardSnapshot,
  BoardSpace,
  ProjectList,
} from './types';

export function listProjects(relayId: string): Promise<ProjectList> {
  return relayStore.boardRpc<ProjectList>(relayId, 'project.list');
}

export function createProject(relayId: string, scopePath: string): Promise<{ board: BoardSnapshot }> {
  return relayStore.boardRpc(relayId, 'project.create', { scope_path: scopePath });
}

export function getBoard(relayId: string, boardId: number): Promise<BoardSnapshot> {
  return relayStore.boardRpc<BoardSnapshot>(relayId, 'board.get', { board_id: boardId });
}

export function createBoard(relayId: string, projectId: number, name: string): Promise<BoardSnapshot> {
  return relayStore.boardRpc<BoardSnapshot>(relayId, 'board.create', { project_id: projectId, name });
}

export function selectBoard(relayId: string, boardId: number): Promise<BoardSnapshot> {
  return relayStore.boardRpc<BoardSnapshot>(relayId, 'board.select', { board_id: boardId });
}

export interface CardDetail {
  card: BoardCard;
  comments: BoardComment[];
  runs: Record<string, unknown>[];
}

export function getCard(relayId: string, cardId: number): Promise<CardDetail> {
  return relayStore.boardRpc<CardDetail>(relayId, 'card.get', { id: cardId });
}

export interface NewCard {
  title: string;
  board_id: number;
  description?: string;
  column_id?: number;
  harness?: string;
  session?: string;
  space_kind?: string;
  space_ref?: string;
}

export function createCard(relayId: string, card: NewCard): Promise<BoardCard> {
  return relayStore.boardRpc<BoardCard>(relayId, 'card.create', { ...card });
}

export function updateCard(
  relayId: string,
  cardId: number,
  fields: Record<string, unknown>,
): Promise<BoardCard> {
  return relayStore.boardRpc<BoardCard>(relayId, 'card.update', { id: cardId, ...fields });
}

/**
 * The one call that starts real work: moving a card into an `auto` column
 * dispatches an agent, which is why the UI always names the destination before
 * asking for it.
 */
export function moveCard(
  relayId: string,
  cardId: number,
  columnId: number,
  position?: number,
): Promise<BoardCard> {
  const params: Record<string, unknown> = { id: cardId, column_id: columnId };
  if (typeof position === 'number') params.position = position;
  return relayStore.boardRpc<BoardCard>(relayId, 'card.move', params);
}

export function archiveCard(relayId: string, cardId: number, archived: boolean): Promise<BoardCard> {
  return relayStore.boardRpc<BoardCard>(relayId, 'card.archive', { id: cardId, archived });
}

export function deleteCard(relayId: string, cardId: number): Promise<{ deleted: boolean }> {
  return relayStore.boardRpc(relayId, 'card.delete', { id: cardId });
}

export function addComment(relayId: string, cardId: number, body: string): Promise<BoardComment> {
  return relayStore.boardRpc<BoardComment>(relayId, 'comment.add', { card_id: cardId, body });
}

export function cancelRun(relayId: string, cardId: number): Promise<unknown> {
  return relayStore.boardRpc(relayId, 'run.cancel', { card_id: cardId });
}

export function retryCard(relayId: string, cardId: number): Promise<unknown> {
  return relayStore.boardRpc(relayId, 'run.retry', { card_id: cardId });
}

export function finishRun(relayId: string, cardId: number, outcome: 'ok' | 'fail'): Promise<unknown> {
  return relayStore.boardRpc(relayId, 'run.done', { card_id: cardId, outcome });
}

export function listHarnesses(relayId: string): Promise<{ harnesses: string[] }> {
  return relayStore.boardRpc(relayId, 'harness.list');
}

export function listSessions(relayId: string): Promise<{ sessions: BoardSession[]; default_label: string }> {
  return relayStore.boardRpc(relayId, 'session.list');
}

export function listSpaces(relayId: string): Promise<{ spaces: BoardSpace[] }> {
  return relayStore.boardRpc(relayId, 'space.list');
}

export function createColumn(
  relayId: string,
  boardId: number,
  name: string,
  trigger: string,
): Promise<BoardColumn> {
  return relayStore.boardRpc<BoardColumn>(relayId, 'column.create', { board_id: boardId, name, trigger });
}
