import { get, writable } from 'svelte/store';
import { shouldRetainSetupFragment } from './config';
import { parseNotificationTarget } from './protocol';
import type { NotificationTarget } from './types';

// The three roots of the tab bar. Everything else is a view pushed over the
// tab it was opened from: a terminal, a card, a launch form, settings. Only the
// roots show the bar, so a pushed view keeps the whole screen.
export type TabId = 'today' | 'agents' | 'board' | 'activity' | 'settings';

export type ViewState =
  | { view: 'agents' }
  | { view: 'settings' }
  | { view: 'workspaces' }
  | { view: 'agents_all' }
  | { view: 'launch'; relayId?: string; workspaceId?: string; cwd?: string }
  | { view: 'activity' }
  | { view: 'activity_detail'; key: string }
  | { view: 'board' }
  | { view: 'board_card'; cardId: number }
  | { view: 'terminal'; paneId: string }
  | { view: 'history'; paneId: string }
  | { view: 'notification'; target: NotificationTarget };

type HistoryViewState = ViewState & {
  herdrView?: boolean;
  index?: number;
};

export const currentView = writable<ViewState>({ view: 'agents' });

const tabRoots: Record<TabId, ViewState> = {
  today: { view: 'agents' },
  agents: { view: 'agents_all' },
  board: { view: 'board' },
  activity: { view: 'activity' },
  settings: { view: 'settings' },
};

// A view either IS a tab root or sits above one. Returning null is what hides
// the tab bar, so a terminal, a card or a settings screen never competes for
// the bottom of the phone with the keyboard and the composer.
export function tabForView(state: ViewState): TabId | null {
  if (state.view === 'agents') return 'today';
  if (state.view === 'agents_all') return 'agents';
  if (state.view === 'board') return 'board';
  if (state.view === 'activity') return 'activity';
  if (state.view === 'settings') return 'settings';
  return null;
}

export function tabRoot(tab: TabId): ViewState {
  return tabRoots[tab];
}
let viewIndex = 0;

function showView(state: ViewState): void {
  if (state.view !== 'agents') window.scrollTo(0, 0);
  currentView.set(state);
}

export function stateFromLocation(locationValue: Pick<Location, 'hash'> = location): ViewState {
  if (locationValue.hash === '#settings') return { view: 'settings' };
  if (locationValue.hash === '#workspaces') return { view: 'workspaces' };
  if (locationValue.hash === '#agents') return { view: 'agents_all' };
  if (locationValue.hash === '#launch') return { view: 'launch' };
  const launchTarget = locationValue.hash.match(/^#launch=(.+)$/);
  if (launchTarget) {
    try {
      const target = JSON.parse(decodeURIComponent(launchTarget[1])) as Record<string, unknown>;
      return {
        view: 'launch',
        relayId: String(target.relayId || ''),
        workspaceId: String(target.workspaceId || ''),
        cwd: String(target.cwd || ''),
      };
    } catch {
      return { view: 'launch' };
    }
  }
  if (locationValue.hash === '#activity') return { view: 'activity' };
  if (locationValue.hash === '#board') return { view: 'board' };
  const boardCard = locationValue.hash.match(/^#card=(\d+)$/);
  if (boardCard) return { view: 'board_card', cardId: Number(boardCard[1]) };
  const activityDetail = locationValue.hash.match(/^#activity=(.+)$/);
  if (activityDetail) {
    try {
      return { view: 'activity_detail', key: decodeURIComponent(activityDetail[1]) };
    } catch {
      return { view: 'activity' };
    }
  }
  const pane = locationValue.hash.match(/^#pane=(.+)$/);
  if (pane) {
    try {
      return { view: 'terminal', paneId: decodeURIComponent(pane[1]) };
    } catch {
      return { view: 'agents' };
    }
  }
  const historyPane = locationValue.hash.match(/^#history=(.+)$/);
  if (historyPane) {
    try {
      return { view: 'history', paneId: decodeURIComponent(historyPane[1]) };
    } catch {
      return { view: 'agents' };
    }
  }
  const notification = locationValue.hash.match(/^#notify=(.+)$/);
  if (notification) {
    const target = parseNotificationTarget(notification[1]);
    if (target) return { view: 'notification', target };
  }
  return { view: 'agents' };
}

export function viewUrl(state: ViewState): string {
  if (state.view === 'settings') return '#settings';
  if (state.view === 'workspaces') return '#workspaces';
  if (state.view === 'agents_all') return '#agents';
  if (state.view === 'launch') {
    if (!state.workspaceId) return '#launch';
    return `#launch=${encodeURIComponent(JSON.stringify({
      relayId: state.relayId || '',
      workspaceId: state.workspaceId,
      cwd: state.cwd || '',
    }))}`;
  }
  if (state.view === 'activity') return '#activity';
  if (state.view === 'board') return '#board';
  if (state.view === 'board_card') return `#card=${state.cardId}`;
  if (state.view === 'activity_detail') return `#activity=${encodeURIComponent(state.key)}`;
  if (state.view === 'terminal') return `#pane=${encodeURIComponent(state.paneId)}`;
  if (state.view === 'history') return `#history=${encodeURIComponent(state.paneId)}`;
  if (state.view === 'notification') return `#notify=${encodeURIComponent(JSON.stringify(state.target))}`;
  return location.pathname + location.search;
}

export function navigate(state: ViewState): void {
  viewIndex += 1;
  history.pushState({ herdrView: true, index: viewIndex, ...state }, '', viewUrl(state));
  showView(state);
}

export function replaceView(state: ViewState): void {
  history.replaceState({ herdrView: true, index: viewIndex, ...state }, '', viewUrl(state));
  showView(state);
}

export function closeCurrentView(): void {
  if (get(currentView).view === 'agents') return;
  const state = history.state as HistoryViewState | null;
  if (viewIndex > 0 && state?.herdrView) history.back();
  else replaceView({ view: 'agents' });
}

export function initializeRouter(): () => void {
  const setupUrl = shouldRetainSetupFragment(location, navigator.standalone)
    ? location.pathname + location.search + location.hash
    : '';
  const initial = stateFromLocation();
  replaceView({ view: 'agents' });
  if (setupUrl) history.replaceState(history.state, '', setupUrl);
  if (initial.view !== 'agents') navigate(initial);
  const onPopState = (event: PopStateEvent) => {
    const state = event.state as HistoryViewState | null;
    viewIndex = Number.isInteger(state?.index) ? Number(state?.index) : 0;
    showView(state?.herdrView ? state : { view: 'agents' });
  };
  const onHashChange = () => showView(stateFromLocation());
  window.addEventListener('popstate', onPopState);
  window.addEventListener('hashchange', onHashChange);
  return () => {
    window.removeEventListener('popstate', onPopState);
    window.removeEventListener('hashchange', onHashChange);
  };
}

export function routeNotificationUrl(url: string): void {
  try {
    const target = new URL(url, location.href);
    if (target.origin !== location.origin || !target.hash) return;
    if (location.hash !== target.hash) location.hash = target.hash;
    else showView(stateFromLocation());
  } catch {
    // Ignore cross-origin and malformed notification URLs.
  }
}
