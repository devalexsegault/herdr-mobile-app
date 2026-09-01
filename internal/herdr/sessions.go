package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// SessionInfo is one Herdr session as `herdr session list --json` reports it.
// A session is a whole server with its own socket; it knows nothing about the
// others, which is why enumeration only exists on the CLI.
type SessionInfo struct {
	Name       string `json:"name"`
	Default    bool   `json:"default"`
	Running    bool   `json:"running"`
	SocketPath string `json:"socket_path"`
}

// ListSessions enumerates the sessions the Herdr binary knows about.
func (c *Client) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	out, err := c.runCommand(ctx, "session", "list", "--json")
	if err != nil {
		return nil, err
	}
	return parseSessionList(out)
}

func parseSessionList(out []byte) ([]SessionInfo, error) {
	var envelope struct {
		Sessions []SessionInfo `json:"sessions"`
		Result   struct {
			Sessions []SessionInfo `json:"sessions"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, fmt.Errorf("malformed session list: %w", err)
	}
	sessions := envelope.Sessions
	if len(sessions) == 0 {
		sessions = envelope.Result.Sessions
	}
	if len(sessions) == 0 {
		return nil, errors.New("session list names no session")
	}
	return sessions, nil
}

// Session-qualified identifiers.
//
// Every Herdr session numbers its workspaces, tabs and panes from one: `w1:p1`
// exists in all of them. The relay therefore prefixes an id with its session
// name -- `hellocare/w1:p1` -- everywhere above the Herdr client, and strips
// the prefix again on the way back down. The base session (the socket the
// relay was configured with) keeps its raw ids, so a relay that only ever sees
// one session emits exactly what it always did.

const sessionSeparator = "/"

// QualifyID prefixes id with the session unless the session is the base one.
func QualifyID(session, id string) string {
	if session == "" || id == "" {
		return id
	}
	return session + sessionSeparator + id
}

// SplitID returns the session an id belongs to and the raw Herdr id. A raw id
// belongs to the base session and comes back with an empty session name.
func SplitID(id string) (session, raw string) {
	session, raw, found := strings.Cut(id, sessionSeparator)
	if !found {
		return "", id
	}
	return session, raw
}

// QualifyTopology rewrites every id in a session's topology in place, so the
// coordinator never sees two sessions collide on `w1`.
func QualifyTopology(session string, workspaces []Workspace, tabs []Tab, panes []Pane) {
	if session == "" {
		return
	}
	for index := range workspaces {
		workspaces[index].ID = QualifyID(session, workspaces[index].ID)
		workspaces[index].ActiveTabID = QualifyID(session, workspaces[index].ActiveTabID)
		workspaces[index].Session = session
	}
	for index := range tabs {
		tabs[index].ID = QualifyID(session, tabs[index].ID)
		tabs[index].WorkspaceID = QualifyID(session, tabs[index].WorkspaceID)
	}
	for index := range panes {
		panes[index].ID = QualifyID(session, panes[index].ID)
		panes[index].TabID = QualifyID(session, panes[index].TabID)
		panes[index].WorkspaceID = QualifyID(session, panes[index].WorkspaceID)
	}
}

// QualifyCreateResult rewrites the ids Herdr hands back for something it just
// created, so the phone addresses the new pane the way the topology names it.
func QualifyCreateResult(session string, result *CreateResult) {
	if result == nil || session == "" {
		return
	}
	result.PaneID = QualifyID(session, result.PaneID)
	result.TabID = QualifyID(session, result.TabID)
	result.WorkspaceID = QualifyID(session, result.WorkspaceID)
}

// Sessions is the set of Herdr sessions one relay serves: the base client it
// was configured with, plus one client per other running session found by
// enumeration. It is safe for concurrent use.
type Sessions struct {
	bin  string
	base *Client

	mu       sync.RWMutex
	baseName string
	clients  map[string]*Client
	infos    []SessionInfo
}

func NewSessions(bin string, base *Client) *Sessions {
	return &Sessions{bin: bin, base: base, clients: make(map[string]*Client)}
}

// Base is the client the relay was configured with. Its ids are never
// qualified.
func (s *Sessions) Base() *Client {
	return s.base
}

// Refresh re-enumerates the running sessions, opening a client for each new
// one and closing the clients of sessions that are gone. It returns the
// running sessions with the base first.
func (s *Sessions) Refresh(ctx context.Context) ([]SessionInfo, error) {
	listed, err := s.base.ListSessions(ctx)
	if err != nil {
		return s.Running(), err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[string]bool, len(listed))
	var infos []SessionInfo
	for _, info := range listed {
		if !info.Running || info.Name == "" || strings.Contains(info.Name, sessionSeparator) {
			continue
		}
		if info.SocketPath == s.base.socketPath {
			s.baseName = info.Name
			s.clients[info.Name] = s.base
		} else if _, ok := s.clients[info.Name]; !ok {
			s.clients[info.Name] = NewClient(s.bin, info.SocketPath)
		}
		seen[info.Name] = true
		infos = append(infos, info)
	}
	for name, client := range s.clients {
		if seen[name] {
			continue
		}
		delete(s.clients, name)
		if client != s.base {
			_ = client.Close()
		}
	}
	sort.SliceStable(infos, func(i, j int) bool {
		if (infos[i].Name == s.baseName) != (infos[j].Name == s.baseName) {
			return infos[i].Name == s.baseName
		}
		return infos[i].Name < infos[j].Name
	})
	s.infos = infos
	return append([]SessionInfo(nil), infos...), nil
}

// Running returns the sessions found by the last Refresh, base first.
func (s *Sessions) Running() []SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]SessionInfo(nil), s.infos...)
}

// Prefix returns the id prefix for a session: empty for the base session,
// the session name for any other.
func (s *Sessions) Prefix(name string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if name == "" || name == s.baseName {
		return ""
	}
	return name
}

// Client returns the client for a session by prefix. The empty prefix is the
// base session; an unknown prefix returns nil.
func (s *Sessions) Client(prefix string) *Client {
	if prefix == "" {
		return s.base
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if prefix == s.baseName {
		return s.base
	}
	return s.clients[prefix]
}

// Resolve maps a possibly qualified id to the client that owns it and the raw
// id that client understands. An id naming a session that is not running
// resolves to a nil client: the caller reports the pane as unavailable
// rather than sending the command to the wrong server.
func (s *Sessions) Resolve(id string) (*Client, string) {
	prefix, raw := SplitID(id)
	return s.Client(prefix), raw
}

// ErrSessionUnavailable reports a qualified id whose session is not running.
var ErrSessionUnavailable = errors.New("herdr: session is not running")

// Unavailable returns a client that can never reach a server. It stands in for
// a session that is no longer running: a raw id such as `w1:p1` also exists in
// every other session, so routing it to the base client would drive a real
// pane on the wrong server. Every call fails on connect instead.
func Unavailable(bin, session string) *Client {
	return NewClient(bin, "/nonexistent/herdr-session-"+session+"/herdr.sock")
}

// ClientFor is Client with the unavailable stand-in for unknown prefixes, for
// callers that only want a value to call methods on.
func (s *Sessions) ClientFor(prefix string) *Client {
	if client := s.Client(prefix); client != nil {
		return client
	}
	return Unavailable(s.bin, prefix)
}

// QualifyWorkspace rewrites one workspace's ids for its session.
func QualifyWorkspace(session string, workspace *Workspace) {
	if workspace == nil || session == "" {
		return
	}
	workspace.ID = QualifyID(session, workspace.ID)
	workspace.ActiveTabID = QualifyID(session, workspace.ActiveTabID)
	workspace.Session = session
}

func qualifyOptionalID(session string, id *string) {
	if id != nil && *id != "" {
		qualified := QualifyID(session, *id)
		*id = qualified
	}
}

// QualifyWorktreeListing rewrites the workspace ids a worktree listing points
// at, so the phone can open them by the name the topology uses.
func QualifyWorktreeListing(session string, listing *WorktreeListResult) {
	if listing == nil || session == "" {
		return
	}
	qualifyOptionalID(session, listing.Source.SourceWorkspaceID)
	for index := range listing.Worktrees {
		qualifyOptionalID(session, listing.Worktrees[index].OpenWorkspaceID)
	}
}

// QualifyWorktreeMutation rewrites the workspace a worktree create or open
// produced.
func QualifyWorktreeMutation(session string, result *WorktreeMutationResult) {
	if result == nil || session == "" {
		return
	}
	QualifyWorkspace(session, &result.Workspace)
}
