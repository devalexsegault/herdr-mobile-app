package app

import (
	"context"
	"strings"
	"testing"

	"github.com/0cv/herdr-mobile-relay/internal/conversation"
	"github.com/0cv/herdr-mobile-relay/internal/coordinator"
)

func TestExplainConversationLeavesAnAvailablePageAlone(t *testing.T) {
	server := testServer()
	page := conversation.Page{Available: true, Reason: ""}

	got := server.explainConversation(context.Background(), &coordinator.AgentState{Agent: "claude"}, page)

	if !got.Available || got.Reason != "" {
		t.Fatalf("page = %+v", got)
	}
}

// Without a Herdr binary the integration list is empty, and the relay must not
// invent an explanation it cannot support.
func TestExplainConversationKeepsTheOriginalReasonWhenNothingIsKnown(t *testing.T) {
	server := testServer()
	page := conversation.Page{Available: false, Reason: "This agent has not reported a conversation session yet."}

	got := server.explainConversation(context.Background(), &coordinator.AgentState{Agent: "claude"}, page)

	if got.Reason != page.Reason {
		t.Fatalf("reason = %q", got.Reason)
	}
}

func TestExplainConversationNamesTheInstallForAMissingHook(t *testing.T) {
	server := testServer()
	server.herdrIntegrations = func(context.Context) []herdrIntegration {
		return []herdrIntegration{{Agent: "claude", Installed: false, State: "not installed"}}
	}
	page := conversation.Page{Available: false, Reason: "This agent has not reported a conversation session yet."}

	got := server.explainConversation(context.Background(), &coordinator.AgentState{Agent: "claude"}, page)

	if !strings.Contains(got.Reason, "herdr integration install claude") {
		t.Fatalf("reason = %q", got.Reason)
	}
}

// An installed hook means the pane simply predates it: restarting is the fix,
// and telling someone to install what they already have is worse than useless.
func TestExplainConversationAsksForARestartWhenTheHookIsInstalled(t *testing.T) {
	server := testServer()
	server.herdrIntegrations = func(context.Context) []herdrIntegration {
		return []herdrIntegration{{Agent: "claude", Installed: true, State: "current (v8)"}}
	}
	page := conversation.Page{Available: false, Reason: "This agent has not reported a conversation session yet."}

	got := server.explainConversation(context.Background(), &coordinator.AgentState{Agent: "claude"}, page)

	if strings.Contains(got.Reason, "integration install") || !strings.Contains(got.Reason, "restart") {
		t.Fatalf("reason = %q", got.Reason)
	}
}
