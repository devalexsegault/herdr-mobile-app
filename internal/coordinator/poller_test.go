package coordinator

import (
	"testing"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/herdr"
)

func TestTopologyStaleRepollsAreBounded(t *testing.T) {
	state := testState()
	state.CommitInventory([]*AgentState{{PaneID: "pane-1", Status: "working"}}, 0)
	poller := NewPoller(nil, state, time.Second, testLogger())

	for retry := 0; retry < maxImmediateTopologyPolls; retry++ {
		poller.handleTopologyStale(state.InventoryStatus())
		select {
		case <-poller.wakeup:
		default:
			t.Fatalf("retry %d did not request an immediate repoll", retry+1)
		}
	}
	poller.handleTopologyStale(state.InventoryStatus())
	select {
	case <-poller.wakeup:
		t.Fatal("topology churn requested an unbounded immediate repoll")
	default:
	}
	status := state.InventoryStatus()
	if status["state"] != "error" || status["error_code"] != "topology_churn" {
		t.Fatalf("inventory status = %+v, want topology degradation", status)
	}
}

// While the event stream is healthy the poll is only a reconcile backstop, but
// when events are unavailable it is the sole freshness source and must honour
// the operator-configured interval again.
func TestPollerIntervalTracksEventStreamHealth(t *testing.T) {
	state := testState()
	state.CommitInventory([]*AgentState{{PaneID: "pane-1", Status: "working"}}, 0)
	poller := NewPoller(nil, state, time.Second, testLogger())

	if got := poller.currentInterval(); got != time.Second {
		t.Fatalf("interval with events down = %v, want the configured 1s", got)
	}

	poller.eventStreams.Store(1)
	if got := poller.currentInterval(); got != idlePollInterval {
		t.Fatalf("interval with events up = %v, want %v", got, idlePollInterval)
	}

	poller.eventStreams.Store(0)
	if got := poller.currentInterval(); got != time.Second {
		t.Fatalf("interval after events dropped = %v, want the configured 1s", got)
	}
}

func TestPollerIntervalClampsToReconcileCeiling(t *testing.T) {
	poller := NewPoller(nil, testState(), time.Hour, testLogger())
	if got := poller.currentInterval(); got != idlePollInterval {
		t.Fatalf("interval = %v, want it clamped to %v", got, idlePollInterval)
	}
}

// An idle machine commits an identical inventory every reconcile interval;
// re-broadcasting it hands every phone a fresh full snapshot to re-render for
// no reason. Only a snapshot that differs from the last broadcast one may go
// out; an explicit refresh_agents request is answered separately.
func TestPollerSkipsUnchangedAgentBroadcasts(t *testing.T) {
	state := testState()
	poller := NewPoller(nil, state, time.Second, testLogger())
	broadcasts := 0
	poller.SetOnChange(func([]*AgentState) { broadcasts++ })

	state.CommitInventory([]*AgentState{{PaneID: "pane-1", Status: "idle"}}, 0)
	poller.notifyAgentsChanged()
	poller.notifyAgentsChanged()
	poller.notifyAgentsChanged()
	if broadcasts != 1 {
		t.Fatalf("broadcasts after identical snapshots = %d, want 1", broadcasts)
	}

	state.CommitInventory([]*AgentState{{PaneID: "pane-1", Status: "working"}}, state.RevisionCounter())
	poller.notifyAgentsChanged()
	if broadcasts != 2 {
		t.Fatalf("broadcasts after a real change = %d, want 2", broadcasts)
	}
}

// Workspace broadcasts read the snapshot under the ordering lock and skip a
// byte-identical repeat, so the reconcile poll and the event stream cannot
// publish a stale topology over a newer one or re-push what clients already
// display.
func TestNotifyWorkspacesChangedSkipsIdenticalTopology(t *testing.T) {
	state := testState()
	poller := NewPoller(nil, state, time.Second, testLogger())
	broadcasts := 0
	poller.SetOnWorkspaceChange(func(workspaces []herdr.Workspace) { broadcasts++ })

	state.CommitWorkspaces([]herdr.Workspace{{ID: "w1", Label: "One"}})
	poller.notifyWorkspacesChanged()
	poller.notifyWorkspacesChanged()
	if broadcasts != 1 {
		t.Fatalf("broadcasts after identical topologies = %d, want 1", broadcasts)
	}

	state.CommitWorkspaces([]herdr.Workspace{{ID: "w1", Label: "Renamed"}})
	poller.notifyWorkspacesChanged()
	if broadcasts != 2 {
		t.Fatalf("broadcasts after a real change = %d, want 2", broadcasts)
	}
}

func TestHydrateWorkspaceCwdsKeepsShellOnlyWorkspaceLaunchable(t *testing.T) {
	workspaces := []herdr.Workspace{{ID: "w1", Label: "Shell only"}}
	hydrateWorkspaceCwds(workspaces, nil, []herdr.Pane{{
		ID: "p1", WorkspaceID: "w1", Cwd: "/home/user/project",
	}})
	if workspaces[0].Cwd != "/home/user/project" {
		t.Fatalf("workspace cwd = %q", workspaces[0].Cwd)
	}
}

func TestAgentsFromTopologyKeepsShellPanesWithoutLifecycle(t *testing.T) {
	agentPanes := []herdr.Pane{{ID: "w1:p1", TabID: "w1:t1", WorkspaceID: "w1", Agent: "claude", Status: "working"}}
	topologyPanes := []herdr.Pane{
		{ID: "w1:p1", TabID: "w1:t1", WorkspaceID: "w1", Agent: "claude"},
		{ID: "w1:p2", TabID: "w1:t1", WorkspaceID: "w1", Status: "idle", Cwd: "/src/app"},
	}
	poller := NewPoller(nil, testState(), time.Second, testLogger())

	agents := poller.agentsFromTopology("", withShellPanes(agentPanes, topologyPanes), []herdr.Tab{{ID: "w1:t1", WorkspaceID: "w1", Label: "main"}})

	if len(agents) != 2 {
		t.Fatalf("agents = %d, want the agent and the shell", len(agents))
	}
	if agents[0].Status != "working" {
		t.Fatalf("agent pane lost its agent-list record: %+v", agents[0])
	}
	shell := agents[1]
	if shell.PaneID != "w1:p2" || !shell.Shell || agents[0].Shell || shell.Status != "" || shell.Project != "app" || shell.TabLabel != "main" {
		t.Fatalf("shell pane = %+v", shell)
	}
}

func TestShellPaneAcceptsInputWithoutTransition(t *testing.T) {
	state := testState()
	transitions := 0
	state.SetOnTransition(func(string, string, string, string, int64) { transitions++ })
	state.CommitInventory([]*AgentState{{PaneID: "w1:p2"}}, 0)
	state.CommitInventory([]*AgentState{{PaneID: "w1:p2"}}, 0)

	if _, active := state.PaneSession("w1:p2"); !active {
		t.Fatal("shell pane is not addressable for keys and text")
	}
	if transitions != 0 {
		t.Fatalf("shell pane fired %d transitions", transitions)
	}
}
