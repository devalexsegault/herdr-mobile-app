package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/0cv/herdr-mobile-relay/internal/conversation"
	"github.com/0cv/herdr-mobile-relay/internal/coordinator"
	"github.com/0cv/herdr-mobile-relay/internal/herdr"
)

type herdrIntegration = herdr.AgentIntegration

// integrations reads the agent state hooks Herdr knows about, through an
// injectable lookup so the explanation is testable without a Herdr binary.
func (s *Server) integrations(ctx context.Context) []herdrIntegration {
	if s.herdrIntegrations != nil {
		return s.herdrIntegrations(ctx)
	}
	return s.herdrC.Integrations(ctx)
}

// explainConversation turns "no session" into something a person can act on.
//
// Herdr never discovers an agent's session by itself: the agent declares it
// through the state hook `herdr integration install` writes into that agent's
// own configuration. When the hook is missing, every agent of that kind is
// permanently without a conversation, and the honest answer names the command
// rather than repeating that nothing was reported.
func (s *Server) explainConversation(
	ctx context.Context,
	agent *coordinator.AgentState,
	page conversation.Page,
) conversation.Page {
	if page.Available || agent == nil {
		return page
	}
	kind := strings.TrimSpace(strings.ToLower(agent.Agent))
	if kind == "" {
		return page
	}
	for _, integration := range s.integrations(ctx) {
		if integration.Agent != kind {
			continue
		}
		if integration.Installed {
			// The hook is there, so this pane simply predates it: restarting the
			// agent is what records a session.
			page.Reason = fmt.Sprintf(
				"%s has not reported a session. Agents started before the Herdr integration was installed keep no transcript; restart this one to record its conversation.",
				kind,
			)
			return page
		}
		page.Reason = fmt.Sprintf(
			"The Herdr integration for %s is not installed on that computer, so it never reports a session and no conversation can be read. Run `herdr integration install %s` there, then restart the agent.",
			kind, kind,
		)
		return page
	}
	return page
}
