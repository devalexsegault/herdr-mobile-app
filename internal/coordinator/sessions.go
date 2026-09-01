package coordinator

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/0cv/herdr-mobile-relay/internal/herdr"
)

// sessionTopology is one Herdr session's latest view, kept per session so a
// change in one session -- a poll or an event -- commits the union of all of
// them rather than wiping the others from the state.
type sessionTopology struct {
	agents     []*AgentState
	workspaces []herdr.Workspace
}

// sessionViews holds the per-session views behind one lock.
type sessionViews struct {
	mu     sync.Mutex
	latest map[string]sessionTopology
}

func newSessionViews() *sessionViews {
	return &sessionViews{latest: make(map[string]sessionTopology)}
}

func (v *sessionViews) set(prefix string, topology sessionTopology) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.latest[prefix] = topology
}

func (v *sessionViews) drop(prefix string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.latest, prefix)
}

// keep drops every session that is not in the running set any more, so the
// panes of a session that stopped disappear with it.
func (v *sessionViews) keep(running map[string]bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for prefix := range v.latest {
		if !running[prefix] {
			delete(v.latest, prefix)
		}
	}
}

// merged returns every session's agents and workspaces, base session first,
// then the others by prefix, so the order the phone sees is stable.
func (v *sessionViews) merged() ([]*AgentState, []herdr.Workspace) {
	v.mu.Lock()
	defer v.mu.Unlock()
	prefixes := make([]string, 0, len(v.latest))
	for prefix := range v.latest {
		prefixes = append(prefixes, prefix)
	}
	sortPrefixes(prefixes)
	var agents []*AgentState
	var workspaces []herdr.Workspace
	for _, prefix := range prefixes {
		agents = append(agents, v.latest[prefix].agents...)
		workspaces = append(workspaces, v.latest[prefix].workspaces...)
	}
	return agents, workspaces
}

func sortPrefixes(prefixes []string) {
	for i := 1; i < len(prefixes); i++ {
		for j := i; j > 0 && lessPrefix(prefixes[j], prefixes[j-1]); j-- {
			prefixes[j], prefixes[j-1] = prefixes[j-1], prefixes[j]
		}
	}
}

// lessPrefix orders the base session (empty prefix) before everything else.
func lessPrefix(a, b string) bool {
	if (a == "") != (b == "") {
		return a == ""
	}
	return a < b
}

// collectSession reads one session's inventory and topology, qualifies the ids
// with the session prefix, and turns the panes into agent states.
func (p *Poller) collectSession(ctx context.Context, client *herdr.Client, prefix string) (sessionTopology, error) {
	inv, err := client.GetInventory(ctx)
	if err != nil {
		return sessionTopology{}, err
	}
	workspaces, err := client.WorkspaceList(ctx)
	if err != nil {
		return sessionTopology{}, err
	}
	tabs, tabErr := client.TabList(ctx)
	if tabErr != nil {
		tabs = nil
	}
	topologyPanes, paneErr := client.PaneList(ctx)
	if paneErr != nil {
		topologyPanes = inv.Panes
	}
	hydrateWorkspaceCwds(workspaces, tabs, topologyPanes)
	herdr.QualifyTopology(prefix, workspaces, tabs, inv.Panes)
	if paneErr == nil {
		herdr.QualifyTopology(prefix, nil, nil, topologyPanes)
	}
	return sessionTopology{
		agents:     p.agentsFromTopology(prefix, inv.Panes, tabs),
		workspaces: workspaces,
	}, nil
}

// projectOf is the project label for a pane: the last path element of its
// working directory.
func projectOf(cwd string) string {
	if cwd == "" {
		return ""
	}
	return filepath.Base(cwd)
}
