package herdr

import (
	"context"
	"strings"
	"sync"
	"time"
)

// AgentIntegration is one agent's Herdr state hook: the small shim Herdr
// installs into an agent's own configuration so the agent reports its session
// id back through `pane.report_agent_session`.
//
// It matters here because Herdr never discovers a session on its own. Without
// the hook the relay is told nothing, cannot locate the agent's transcript, and
// the conversation view has nothing to read — which looks to a person like a
// broken button rather than a missing one-line install.
type AgentIntegration struct {
	Agent     string `json:"agent"`
	Installed bool   `json:"installed"`
	State     string `json:"state"`
}

const integrationsTTL = 60 * time.Second

var integrationsCache struct {
	sync.Mutex
	at    time.Time
	value []AgentIntegration
}

// Integrations reports what `herdr integration status` knows, cached because it
// shells out and every app connection asks for it. A failure is reported as an
// empty list: the relay says nothing rather than claiming an integration is
// missing on the strength of a command that did not run.
func (c *Client) Integrations(ctx context.Context) []AgentIntegration {
	integrationsCache.Lock()
	defer integrationsCache.Unlock()
	if time.Since(integrationsCache.at) < integrationsTTL {
		return integrationsCache.value
	}
	statusCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := c.runCommand(statusCtx, "integration", "status")
	integrationsCache.at = time.Now()
	if err != nil {
		integrationsCache.value = nil
		return nil
	}
	integrationsCache.value = parseIntegrationStatus(string(out))
	return integrationsCache.value
}

// parseIntegrationStatus reads the command's plain output, one agent per line:
//
//	claude: current (v8) (/home/you/.claude/hooks/herdr-agent-state.sh)
//	codex: not installed (/home/you/.codex/herdr-agent-state.sh)
//
// The trailing path is dropped: it names a file on the other computer, which
// is of no use to a phone and is not something to put on the wire.
func integrationName(value string) bool {
	if value == "" {
		return false
	}
	for _, letter := range value {
		switch {
		case letter >= 'a' && letter <= 'z', letter >= '0' && letter <= '9', letter == '-', letter == '_':
		default:
			return false
		}
	}
	return true
}

func parseIntegrationStatus(output string) []AgentIntegration {
	var integrations []AgentIntegration
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, rest, found := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		rest = strings.TrimSpace(rest)
		if !found || !integrationName(name) {
			continue
		}
		// Every real line ends with the hook's path in parentheses. Requiring it
		// is what keeps the command's help text -- which also contains colons and
		// URLs -- out of the list.
		index := strings.LastIndex(rest, " (/")
		if index <= 0 || !strings.HasSuffix(rest, ")") {
			continue
		}
		rest = strings.TrimSpace(rest[:index])
		integrations = append(integrations, AgentIntegration{
			Agent:     name,
			Installed: !strings.HasPrefix(strings.ToLower(rest), "not installed"),
			State:     rest,
		})
	}
	return integrations
}
