package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/herdr"
)

// Two sessions each own a `w1:p1`. With the overlay on, the poller commits
// the union of both, with the other session's ids qualified, and a change in
// one session never wipes the other from the state.
func TestPollerMergesSessionsAndKeepsBaseIdsRaw(t *testing.T) {
	state := testState()
	poller := NewPoller(nil, state, time.Second, testLogger())
	poller.SetSessions(herdr.NewSessions("herdr", nil))

	base := herdr.TopologySnapshot{
		Workspaces: []herdr.Workspace{{ID: "w1", Label: "Main"}},
		Tabs:       []herdr.Tab{{ID: "w1:t1", WorkspaceID: "w1", Label: "main"}},
		Panes:      []herdr.Pane{{ID: "w1:p1", TabID: "w1:t1", WorkspaceID: "w1", Agent: "claude", Status: "working", Cwd: "/home/me/base"}},
	}
	other := herdr.TopologySnapshot{
		Workspaces: []herdr.Workspace{{ID: "w1", Label: "front"}},
		Tabs:       []herdr.Tab{{ID: "w1:t1", WorkspaceID: "w1", Label: "front"}},
		Panes:      []herdr.Pane{{ID: "w1:p1", TabID: "w1:t1", WorkspaceID: "w1", Agent: "codex", Status: "idle", Cwd: "/home/me/front"}},
	}

	poller.commitEventTopology(context.Background(), "", base, state.RevisionCounter())
	poller.commitEventTopology(context.Background(), "hellocare", other, state.RevisionCounter())

	agents := state.Snapshot()
	if len(agents) != 2 {
		t.Fatalf("agents = %d, want both sessions: %+v", len(agents), agents)
	}
	byID := make(map[string]*AgentState)
	for _, agent := range agents {
		byID[agent.PaneID] = agent
	}
	if _, ok := byID["w1:p1"]; !ok {
		t.Fatalf("the base session's pane must keep its raw id: %v", byID)
	}
	qualified, ok := byID["hellocare/w1:p1"]
	if !ok {
		t.Fatalf("the other session's pane must be qualified: %v", byID)
	}
	if qualified.HerdrSession != "hellocare" || qualified.WorkspaceID != "hellocare/w1" || qualified.TabID != "hellocare/w1:t1" {
		t.Fatalf("qualified agent = %+v", qualified)
	}
	if byID["w1:p1"].HerdrSession != "" {
		t.Fatalf("base agent carries a session: %+v", byID["w1:p1"])
	}

	workspaces := state.Workspaces()
	if len(workspaces) != 2 || workspaces[0].ID != "w1" || workspaces[1].ID != "hellocare/w1" || workspaces[1].Session != "hellocare" {
		t.Fatalf("workspaces = %+v", workspaces)
	}

	// The base session changes: the other session's pane survives the commit.
	base.Panes[0].Status = "done"
	poller.commitEventTopology(context.Background(), "", base, state.RevisionCounter())
	if got := len(state.Snapshot()); got != 2 {
		t.Fatalf("a base commit dropped the other session: %d agents", got)
	}
}

// A session that stops running takes its panes with it.
func TestSessionViewsForgetSessionsThatAreGone(t *testing.T) {
	views := newSessionViews()
	views.set("", sessionTopology{agents: []*AgentState{{PaneID: "w1:p1"}}})
	views.set("gone", sessionTopology{agents: []*AgentState{{PaneID: "gone/w1:p1"}}})

	views.keep(map[string]bool{"": true})

	agents, _ := views.merged()
	if len(agents) != 1 || agents[0].PaneID != "w1:p1" {
		t.Fatalf("agents = %+v", agents)
	}
}

// Without the overlay, ids and behaviour are exactly what they were.
func TestPollerWithoutSessionsKeepsRawIds(t *testing.T) {
	state := testState()
	poller := NewPoller(nil, state, time.Second, testLogger())
	topology := herdr.TopologySnapshot{
		Panes: []herdr.Pane{{ID: "w1:p1", Agent: "claude", Status: "working"}},
	}
	poller.commitEventTopology(context.Background(), "", topology, state.RevisionCounter())
	agents := state.Snapshot()
	if len(agents) != 1 || agents[0].PaneID != "w1:p1" || agents[0].HerdrSession != "" {
		t.Fatalf("agents = %+v", agents)
	}
}
