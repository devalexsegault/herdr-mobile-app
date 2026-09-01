package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/herdr"
)

const (
	idlePollInterval          = 15 * time.Second
	maxImmediateTopologyPolls = 3
)

type Poller struct {
	client              *herdr.Client
	sessions            *herdr.Sessions
	views               *sessionViews
	state               *State
	logger              *slog.Logger
	interval            time.Duration
	wakeup              chan struct{}
	onChange            func(agents []*AgentState)
	onWorkspaceChange   func(workspaces []herdr.Workspace)
	onStatus            func(status map[string]any)
	enrich              func(context.Context, []*AgentState)
	hostname            string
	topologyRetries     int
	consecutiveFailures atomic.Int32
	// eventStreams counts the live Herdr event streams, one per session. While
	// any is up the reconcile poll slows down; with none, it is the only
	// freshness source again.
	eventStreams atomic.Int32
	// broadcastMu serializes snapshot broadcasts from the reconcile poll and
	// the event stream, and guards the dedupe state below. Snapshots are read
	// inside the lock so a slow commit path can never publish an older
	// topology after a newer one already went out.
	broadcastMu        sync.Mutex
	lastAgentsJSON     []byte
	lastWorkspacesJSON []byte
}

func NewPoller(client *herdr.Client, state *State, interval time.Duration, logger *slog.Logger) *Poller {
	hostname, _ := os.Hostname()
	if idx := strings.Index(hostname, "."); idx > 0 {
		hostname = hostname[:idx]
	}
	return &Poller{
		client:   client,
		state:    state,
		logger:   logger,
		interval: interval,
		wakeup:   make(chan struct{}, 1),
		hostname: hostname,
		views:    newSessionViews(),
	}
}

// SetSessions installs the session overlay: the poll and the event streams
// then cover every running Herdr session, with ids qualified by session.
func (p *Poller) SetSessions(sessions *herdr.Sessions) {
	p.sessions = sessions
}

func (p *Poller) SetOnChange(fn func(agents []*AgentState)) {
	p.onChange = fn
}

func (p *Poller) SetOnWorkspaceChange(fn func(workspaces []herdr.Workspace)) {
	p.onWorkspaceChange = fn
}

func (p *Poller) SetOnInventoryStatus(fn func(status map[string]any)) {
	p.onStatus = fn
}

func (p *Poller) SetEnrich(fn func(context.Context, []*AgentState)) {
	p.enrich = fn
}

func (p *Poller) Wake() {
	select {
	case p.wakeup <- struct{}{}:
	default:
	}
}

func (p *Poller) ConsecutiveFailures() int {
	return int(p.consecutiveFailures.Load())
}

func (p *Poller) Run(ctx context.Context) {
	p.poll(ctx)

	timer := time.NewTimer(p.currentInterval())
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.wakeup:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			p.poll(ctx)
			timer.Reset(p.currentInterval())
		case <-timer.C:
			p.poll(ctx)
			timer.Reset(p.currentInterval())
		}
	}
}

func (p *Poller) poll(ctx context.Context) {
	if p.sessions != nil {
		p.pollSessions(ctx)
		return
	}
	token := p.state.BeginPoll()
	previousStatus := p.state.InventoryStatus()

	inv, err := p.client.GetInventory(ctx)
	if err != nil {
		p.consecutiveFailures.Add(1)
		p.state.MarkInventoryFailure(err)
		p.notifyStatusChange(previousStatus)
		p.logger.Warn("inventory poll failed", "error", err)
		return
	}
	p.consecutiveFailures.Store(0)

	workspaces, err := p.client.WorkspaceList(ctx)
	if err != nil {
		p.consecutiveFailures.Add(1)
		p.state.MarkInventoryFailure(err)
		p.notifyStatusChange(previousStatus)
		p.logger.Warn("workspace inventory poll failed", "error", err)
		return
	}

	tabs, tabErr := p.client.TabList(ctx)
	if tabErr != nil {
		tabs = nil
	}
	topologyPanes, paneErr := p.client.PaneList(ctx)
	if paneErr != nil {
		topologyPanes = inv.Panes
	}
	hydrateWorkspaceCwds(workspaces, tabs, topologyPanes)
	agents := p.agentsFromTopology("", inv.Panes, tabs)

	if p.enrich != nil {
		p.enrich(ctx, agents)
	}

	workspaceChanged, committed := p.state.CommitPoll(agents, workspaces, token)
	if !committed {
		p.logger.Debug("discarded topology-stale inventory sample")
		p.handleTopologyStale(previousStatus)
		return
	}
	p.topologyRetries = 0
	p.notifyStatusChange(previousStatus)
	p.logger.Debug("inventory committed", "agents", len(agents), "topology", p.state.TopologyGeneration())

	p.notifyAgentsChanged()
	if workspaceChanged {
		p.notifyWorkspacesChanged()
	}
}

// pollSessions is the reconcile poll across every running session. The base
// session keeps the failure semantics the single-session poll had: if it
// cannot be read, the inventory is marked failed. Another session that cannot
// be read simply drops out until it answers again.
func (p *Poller) pollSessions(ctx context.Context) {
	token := p.state.BeginPoll()
	previousStatus := p.state.InventoryStatus()

	infos, refreshErr := p.sessions.Refresh(ctx)
	if refreshErr != nil {
		p.logger.Debug("herdr session enumeration failed; polling the base session only", "error", refreshErr)
	}
	running := map[string]bool{"": true}
	for _, info := range infos {
		running[p.sessions.Prefix(info.Name)] = true
	}
	for prefix := range running {
		client := p.sessions.Client(prefix)
		if client == nil {
			continue
		}
		topology, err := p.collectSession(ctx, client, prefix)
		if err != nil {
			if prefix == "" {
				p.consecutiveFailures.Add(1)
				p.state.MarkInventoryFailure(err)
				p.notifyStatusChange(previousStatus)
				p.logger.Warn("inventory poll failed", "error", err)
				return
			}
			p.logger.Warn("session inventory poll failed", "session", prefix, "error", err)
			p.views.drop(prefix)
			continue
		}
		p.views.set(prefix, topology)
	}
	p.consecutiveFailures.Store(0)
	p.views.keep(running)
	agents, workspaces := p.views.merged()

	if p.enrich != nil {
		p.enrich(ctx, agents)
	}

	workspaceChanged, committed := p.state.CommitPoll(agents, workspaces, token)
	if !committed {
		p.logger.Debug("discarded topology-stale inventory sample")
		p.handleTopologyStale(previousStatus)
		return
	}
	p.topologyRetries = 0
	p.notifyStatusChange(previousStatus)
	p.logger.Debug("inventory committed", "agents", len(agents), "sessions", len(running), "topology", p.state.TopologyGeneration())

	p.notifyAgentsChanged()
	if workspaceChanged {
		p.notifyWorkspacesChanged()
	}
}

func (p *Poller) agentsFromTopology(session string, panes []herdr.Pane, tabs []herdr.Tab) []*AgentState {
	tabByID := make(map[string]herdr.Tab, len(tabs))
	// 1-based visual position per workspace; tab numbers are stable
	// identities and never reflect moves.
	tabOrderByID := make(map[string]int, len(tabs))
	perWorkspace := make(map[string]int)
	for index, tab := range tabs {
		if tab.Number == 0 {
			tab.Number = index + 1
		}
		tabByID[tab.ID] = tab
		perWorkspace[tab.WorkspaceID]++
		tabOrderByID[tab.ID] = perWorkspace[tab.WorkspaceID]
	}

	agents := make([]*AgentState, 0, len(panes))
	for _, pane := range panes {
		if pane.Agent == "" {
			continue
		}
		if tab, ok := tabByID[pane.TabID]; ok {
			pane.TabLabel = tab.Label
			pane.TabNumber = tab.Number
		}
		project := ""
		if pane.Cwd != "" {
			project = filepath.Base(pane.Cwd)
		}
		agents = append(agents, &AgentState{
			PaneID:          pane.ID,
			RawPaneID:       pane.ID,
			TerminalID:      pane.TerminalID,
			TabID:           pane.TabID,
			TabLabel:        pane.TabLabel,
			TabNumber:       pane.TabNumber,
			TabOrder:        tabOrderByID[pane.TabID],
			WorkspaceID:     pane.WorkspaceID,
			Agent:           pane.Agent,
			Name:            pane.Name,
			Status:          pane.Status,
			Focused:         pane.Focused,
			Cwd:             pane.Cwd,
			Project:         project,
			Host:            p.hostname,
			HerdrSession:    session,
			Session:         pane.Session,
			ActivitySeq:     pane.StateChangeSeq,
			PaneRevision:    pane.Revision,
			ScrollMaxOffset: pane.Scroll.MaxOffsetFromBottom,
			ForegroundCwd:   pane.ForegroundCwd,
		})
	}
	return agents
}

func hydrateWorkspaceCwds(workspaces []herdr.Workspace, tabs []herdr.Tab, panes []herdr.Pane) {
	cwds := make(map[string]string, len(workspaces))
	for _, tab := range tabs {
		if tab.WorkspaceID != "" {
			cwds[tab.WorkspaceID] = shorterPath(cwds[tab.WorkspaceID], tab.Cwd)
		}
	}
	for _, pane := range panes {
		if pane.WorkspaceID != "" {
			cwds[pane.WorkspaceID] = shorterPath(cwds[pane.WorkspaceID], pane.Cwd)
		}
	}
	for index := range workspaces {
		if workspaces[index].Worktree != nil && workspaces[index].Worktree.CheckoutPath != "" {
			workspaces[index].Cwd = workspaces[index].Worktree.CheckoutPath
			continue
		}
		workspaces[index].Cwd = cwds[workspaces[index].ID]
	}
}

func shorterPath(current, candidate string) string {
	if candidate == "" || current != "" && len(current) <= len(candidate) {
		return current
	}
	return candidate
}

func (p *Poller) RunEvents(ctx context.Context, events *herdr.EventClient) {
	p.runEvents(ctx, "", events)
}

// RunSessionEvents keeps one event stream per running session, starting a
// stream when a session appears and stopping it when the session goes away.
// The probe decides whether workspace.reordered may be subscribed; it is the
// same Herdr binary for every session, so one answer serves all of them.
func (p *Poller) RunSessionEvents(ctx context.Context, reorderedProbe func() bool) {
	if p.sessions == nil {
		return
	}
	type stream struct {
		cancel context.CancelFunc
		done   chan struct{}
	}
	streams := make(map[string]*stream)
	defer func() {
		for _, running := range streams {
			running.cancel()
			<-running.done
		}
	}()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		wanted := make(map[string]herdr.SessionInfo)
		for _, info := range p.sessions.Running() {
			wanted[p.sessions.Prefix(info.Name)] = info
		}
		for prefix, running := range streams {
			if _, ok := wanted[prefix]; ok {
				continue
			}
			running.cancel()
			<-running.done
			delete(streams, prefix)
			p.views.drop(prefix)
		}
		for prefix, info := range wanted {
			if _, ok := streams[prefix]; ok {
				continue
			}
			streamCtx, cancel := context.WithCancel(ctx)
			running := &stream{cancel: cancel, done: make(chan struct{})}
			streams[prefix] = running
			events := herdr.NewEventClient(info.SocketPath)
			events.SetWorkspaceReorderedProbe(reorderedProbe)
			go func(prefix string) {
				defer close(running.done)
				p.runEvents(streamCtx, prefix, events)
			}(prefix)
		}
		timer.Reset(idlePollInterval)
	}
}

func (p *Poller) runEvents(ctx context.Context, prefix string, events *herdr.EventClient) {
	if events == nil {
		return
	}
	live := false
	setLive := func(next bool) {
		if next == live {
			return
		}
		live = next
		if next {
			p.eventStreams.Add(1)
		} else {
			p.eventStreams.Add(-1)
		}
	}
	defer setLive(false)
	for {
		if ctx.Err() != nil {
			return
		}
		baseRevision := p.state.RevisionCounter()
		stream, snapshot, buffered, err := events.Bootstrap(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// The reconcile poll is the only freshness source until the stream
			// is back, so let it run at the configured interval again.
			setLive(false)
			p.logger.Warn("Herdr events stream unavailable", "session", prefix, "error", err)
			if !waitForEventReconnect(ctx) {
				return
			}
			continue
		}
		setLive(true)

		cache := herdr.NewSessionCache(snapshot)
		p.commitEventTopology(ctx, prefix, cache.Snapshot(), baseRevision)
		reconnect := false
		for _, event := range buffered {
			if !p.applyTopologyEvent(ctx, prefix, cache, event) {
				reconnect = true
				break
			}
		}
		for !reconnect {
			event, err := stream.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					_ = stream.Close()
					return
				}
				p.logger.Warn("Herdr events stream dropped", "session", prefix, "error", err)
				reconnect = true
				break
			}
			if !p.applyTopologyEvent(ctx, prefix, cache, event) {
				reconnect = true
			}
		}
		setLive(false)
		_ = stream.Close()
		if !waitForEventReconnect(ctx) {
			return
		}
	}
}

func (p *Poller) applyTopologyEvent(ctx context.Context, prefix string, cache *herdr.SessionCache, event herdr.Event) bool {
	changed, err := cache.Apply(event)
	if err != nil {
		p.logger.Warn("Herdr topology event decode failed", "event", event.Event, "error", err)
		return false
	}
	if !changed {
		return true
	}
	p.commitEventTopology(ctx, prefix, cache.Snapshot(), p.state.RevisionCounter())
	return true
}

func (p *Poller) commitEventTopology(ctx context.Context, prefix string, topology herdr.TopologySnapshot, baseRevision int64) {
	previousStatus := p.state.InventoryStatus()
	herdr.QualifyTopology(prefix, topology.Workspaces, topology.Tabs, topology.Panes)
	agents := p.agentsFromTopology(prefix, topology.Panes, topology.Tabs)
	workspaces := topology.Workspaces
	if p.sessions != nil {
		// One session changed; the state is the union of all of them.
		p.views.set(prefix, sessionTopology{agents: agents, workspaces: workspaces})
		agents, workspaces = p.views.merged()
	}
	if p.enrich != nil {
		p.enrich(ctx, agents)
	}
	p.consecutiveFailures.Store(0)
	workspaceChanged := p.state.CommitTopology(agents, workspaces, baseRevision)
	p.notifyStatusChange(previousStatus)
	p.logger.Debug("event inventory committed", "session", prefix, "agents", len(agents), "workspaces", len(workspaces), "topology", p.state.TopologyGeneration())
	p.notifyAgentsChanged()
	if workspaceChanged {
		p.notifyWorkspacesChanged()
	}
}

// notifyAgentsChanged broadcasts the current agent snapshot unless nothing a
// client renders has changed since the last broadcast. Both freshness sources
// — the reconcile poll and the Herdr event stream — commit through here, so
// an idle machine stops producing a full `agents` push every interval and
// phones on metered or fragile links receive silence instead of a
// re-shuffled copy of what they already display.
//
// StateRevision is excluded from the comparison: every commit stamps every
// agent with the new revision counter, so including it would re-broadcast
// identical inventories forever. A suppressed revision-only bump is safe —
// clients only reject revisions that move backwards.
func (p *Poller) notifyAgentsChanged() {
	if p.onChange == nil {
		return
	}
	p.broadcastMu.Lock()
	defer p.broadcastMu.Unlock()
	snapshot := p.state.Snapshot()
	comparable := make([]AgentState, len(snapshot))
	for i, agent := range snapshot {
		comparable[i] = *agent
		comparable[i].StateRevision = 0
	}
	encoded, err := json.Marshal(comparable)
	if err == nil {
		if bytes.Equal(encoded, p.lastAgentsJSON) {
			return
		}
		p.lastAgentsJSON = encoded
	}
	p.onChange(snapshot)
}

// notifyWorkspacesChanged mirrors notifyAgentsChanged for workspace
// broadcasts: the snapshot is read under broadcastMu so the poll and event
// commits publish in commit order, and a byte-identical broadcast — both
// sources committing the same topology back to back — is suppressed.
func (p *Poller) notifyWorkspacesChanged() {
	if p.onWorkspaceChange == nil {
		return
	}
	p.broadcastMu.Lock()
	defer p.broadcastMu.Unlock()
	workspaces := p.state.Workspaces()
	encoded, err := json.Marshal(workspaces)
	if err == nil {
		if bytes.Equal(encoded, p.lastWorkspacesJSON) {
			return
		}
		p.lastWorkspacesJSON = encoded
	}
	p.onWorkspaceChange(workspaces)
}

func waitForEventReconnect(ctx context.Context) bool {
	timer := time.NewTimer(idlePollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (p *Poller) handleTopologyStale(previousStatus map[string]any) {
	p.topologyRetries++
	if p.topologyRetries <= maxImmediateTopologyPolls {
		p.Wake()
		return
	}
	p.state.MarkTopologyDegraded()
	p.notifyStatusChange(previousStatus)
	p.logger.Warn("inventory topology did not stabilize", "immediate_retries", maxImmediateTopologyPolls)
}

func (p *Poller) notifyStatusChange(previous map[string]any) {
	current := p.state.InventoryStatus()
	if p.onStatus != nil && inventoryStatusChanged(previous, current) {
		p.onStatus(current)
	}
}

func inventoryStatusChanged(previous, current map[string]any) bool {
	for _, key := range []string{"state", "error_code", "message", "stale"} {
		if previous[key] != current[key] {
			return true
		}
	}
	return false
}

// currentInterval keeps the reconcile poll slow while the event stream is
// healthy. When events are unavailable the poll is the only freshness source
// again, so the operator-configured interval is honoured.
func (p *Poller) currentInterval() time.Duration {
	if p.eventStreams.Load() > 0 {
		return idlePollInterval
	}
	if p.interval <= 0 || p.interval > idlePollInterval {
		return idlePollInterval
	}
	return p.interval
}
