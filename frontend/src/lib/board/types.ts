/**
 * Mirrors of the herdr-board protocol v1 payloads the app reads.
 *
 * The daemon's `board-core::protocol` module is the source of truth; these
 * types were written against real responses from a running boardd and stay
 * deliberately permissive (`[key: string]: unknown`) so a field added by a
 * newer board release travels through the relay untouched instead of being
 * dropped on the floor here.
 */

export interface BoardSummary {
  id: number;
  name: string;
  project_id: number;
  scope_path: string | null;
  archived_at: string | null;
}

export interface ProjectSummary {
  id: number;
  name: string;
  scope_path: string | null;
  archived_at: string | null;
}

export interface ProjectEntry {
  project: ProjectSummary;
  boards: BoardSummary[];
  selected_board_id?: number;
  recent_board_ids?: number[];
}

export interface ProjectList {
  projects: ProjectEntry[];
  selected_project_id?: number;
  recent_project_ids?: number[];
}

/**
 * `trigger` is what makes a column dangerous: dropping a card into an `auto`
 * column dispatches a real agent, so the UI never treats a move as cosmetic.
 */
export interface BoardColumn {
  id: number;
  board_id: number;
  name: string;
  position: number;
  trigger: string;
  fresh_session: boolean;
  harness_override: string | null;
  model_override: string | null;
  effort_override: string | null;
  permission_override: string | null;
  system_prompt: string | null;
  on_success_column_id: number | null;
  on_fail_column_id: number | null;
  timeout_minutes: number | null;
  [key: string]: unknown;
}

export type CardStatus = 'idle' | 'queued' | 'running' | 'blocked' | 'awaiting' | 'done' | 'failed';

export interface BoardCard {
  id: number;
  board_id: number;
  column_id: number;
  title: string;
  description: string;
  status: CardStatus | string;
  position: number;
  harness: string;
  model: string;
  effort: string;
  permission_mode: string;
  session: string | null;
  session_id: string | null;
  space_kind: string;
  space_ref: string;
  space_cwd: string;
  labels: Record<string, unknown>;
  awaiting_reason: string | null;
  created_at: string;
  updated_at: string;
  archived_at: string | null;
  [key: string]: unknown;
}

export interface ActiveRun {
  card_id: number;
  started_at: string;
  [key: string]: unknown;
}

export interface BoardSnapshot {
  board: BoardSummary;
  columns: BoardColumn[];
  cards: BoardCard[];
  active_runs: ActiveRun[];
}

export interface BoardComment {
  id: number;
  card_id: number;
  body: string;
  author?: string;
  created_at: string;
  [key: string]: unknown;
}

export interface BoardSession {
  name: string;
  default: boolean;
  running: boolean;
}

export interface BoardSpace {
  id: string;
  label: string;
}

/** The relay's board descriptor, sent with the connect handshake. */
export interface BoardDescriptor {
  version: string;
  herdr_connected: boolean;
  active_runs: number;
  queued_runs: number;
  methods: string[];
}

/** A boardd protocol error, or the relay's own code 0 for "never answered". */
export interface BoardErrorPayload {
  code: number;
  kind?: string;
  message: string;
  details?: unknown;
}
