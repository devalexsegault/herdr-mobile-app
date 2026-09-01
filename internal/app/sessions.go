package app

import (
	"context"

	"github.com/0cv/herdr-mobile-relay/internal/herdr"
)

// paneClient returns the Herdr client that owns a possibly session-qualified
// pane id, and the raw id that client understands. A session that is gone
// answers with a client that cannot connect rather than the base client: the
// raw id also names a pane there.
func (s *Server) paneClient(paneID string) (*herdr.Client, string) {
	if s.herdrSessions == nil {
		return s.herdrC, paneID
	}
	prefix, raw := herdr.SplitID(paneID)
	return s.herdrSessions.ClientFor(prefix), raw
}

// sessionPanes routes pane process lookups -- the basis of pane size leases --
// to the session that owns the pane.
type sessionPanes struct {
	sessions *herdr.Sessions
}

func (r sessionPanes) PaneProcessInfo(ctx context.Context, paneID string) (*herdr.PaneProcessInfo, error) {
	client, raw := r.sessions.Resolve(paneID)
	if client == nil {
		return nil, herdr.ErrSessionUnavailable
	}
	return client.PaneProcessInfo(ctx, raw)
}

// sessionDescriptors is the handshake's view of the sessions: their names and
// the id prefix each one uses, base first.
func (s *Server) sessionDescriptors() []map[string]any {
	if s.herdrSessions == nil {
		return nil
	}
	running := s.herdrSessions.Running()
	if len(running) == 0 {
		return nil
	}
	descriptors := make([]map[string]any, 0, len(running))
	for _, info := range running {
		descriptors = append(descriptors, map[string]any{
			"name":    info.Name,
			"prefix":  s.herdrSessions.Prefix(info.Name),
			"default": info.Default,
		})
	}
	return descriptors
}
