package herdr

import "testing"

func TestParseIntegrationStatus(t *testing.T) {
	got := parseIntegrationStatus(`pi: not installed (/home/you/.pi/agent/extensions/herdr-agent-state.ts)
claude: current (v8) (/home/you/.claude/hooks/herdr-agent-state.sh)
codex: outdated (v6 < v8) (/home/you/.codex/herdr-agent-state.sh)

`)
	if len(got) != 3 {
		t.Fatalf("parsed %d lines: %+v", len(got), got)
	}
	if got[0] != (AgentIntegration{Agent: "pi", Installed: false, State: "not installed"}) {
		t.Errorf("pi = %+v", got[0])
	}
	if got[1] != (AgentIntegration{Agent: "claude", Installed: true, State: "current (v8)"}) {
		t.Errorf("claude = %+v", got[1])
	}
	// An outdated hook is installed: it reports sessions, just not the newest
	// contract, so it must not be advertised as missing.
	if !got[2].Installed || got[2].State != "outdated (v6 < v8)" {
		t.Errorf("codex = %+v", got[2])
	}
	// The path is dropped: it names a file on someone else's computer.
	for _, integration := range got {
		if len(integration.State) > 0 && integration.State[0] == '/' {
			t.Errorf("state kept a path: %+v", integration)
		}
	}
}

func TestParseIntegrationStatusIgnoresNoise(t *testing.T) {
	got := parseIntegrationStatus("Are you an AI? Use these resources ONLY IF:\n  https://herdr.dev/llms.txt\nclaude: current (v8) (/tmp/hook.sh)\n")
	if len(got) != 1 || got[0].Agent != "claude" {
		t.Fatalf("parsed %+v", got)
	}
}
