package herdr

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestQualifyAndSplitID(t *testing.T) {
	if got := QualifyID("", "w1:p1"); got != "w1:p1" {
		t.Fatalf("base session must keep raw ids, got %q", got)
	}
	if got := QualifyID("hellocare", "w1:p1"); got != "hellocare/w1:p1" {
		t.Fatalf("QualifyID = %q", got)
	}
	session, raw := SplitID("hellocare/w1:p1")
	if session != "hellocare" || raw != "w1:p1" {
		t.Fatalf("SplitID = %q %q", session, raw)
	}
	session, raw = SplitID("w1:p1")
	if session != "" || raw != "w1:p1" {
		t.Fatalf("raw id must map to the base session, got %q %q", session, raw)
	}
}

func TestQualifyTopologyRewritesEveryReference(t *testing.T) {
	workspaces := []Workspace{{ID: "w1", ActiveTabID: "w1:t1"}}
	tabs := []Tab{{ID: "w1:t1", WorkspaceID: "w1"}}
	panes := []Pane{{ID: "w1:p1", TabID: "w1:t1", WorkspaceID: "w1"}}

	QualifyTopology("hellocare", workspaces, tabs, panes)

	if workspaces[0].ID != "hellocare/w1" || workspaces[0].ActiveTabID != "hellocare/w1:t1" || workspaces[0].Session != "hellocare" {
		t.Fatalf("workspace = %+v", workspaces[0])
	}
	if tabs[0].ID != "hellocare/w1:t1" || tabs[0].WorkspaceID != "hellocare/w1" {
		t.Fatalf("tab = %+v", tabs[0])
	}
	if panes[0].ID != "hellocare/w1:p1" || panes[0].TabID != "hellocare/w1:t1" || panes[0].WorkspaceID != "hellocare/w1" {
		t.Fatalf("pane = %+v", panes[0])
	}
	// The base session is left exactly as Herdr reported it.
	base := []Pane{{ID: "w1:p1"}}
	QualifyTopology("", nil, nil, base)
	if base[0].ID != "w1:p1" {
		t.Fatalf("base pane = %+v", base[0])
	}
}

func TestParseSessionListAcceptsBothEnvelopes(t *testing.T) {
	plain := []byte(`{"sessions":[{"default":true,"name":"default","running":true,"socket_path":"/tmp/a.sock"}]}`)
	wrapped := []byte(`{"id":"x","result":{"sessions":[{"name":"hellocare","running":true,"socket_path":"/tmp/b.sock"}]}}`)
	for _, out := range [][]byte{plain, wrapped} {
		sessions, err := parseSessionList(out)
		if err != nil || len(sessions) != 1 {
			t.Fatalf("parse %s: %v %+v", out, err, sessions)
		}
	}
	if _, err := parseSessionList([]byte(`{"sessions":[]}`)); err == nil {
		t.Fatal("an empty list must be an error, not a silent single-session relay")
	}
}

// A fake `herdr` whose only job is to answer `session list --json`.
func fakeSessionBinary(t *testing.T, sessions string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "herdr")
	script := "#!/bin/sh\nif [ \"$1\" = session ]; then printf '%s' '" + sessions + "'; exit 0; fi\necho unsupported >&2; exit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestSessionsRefreshRoutesByPrefixAndKeepsBaseRaw(t *testing.T) {
	baseSock := filepath.Join(t.TempDir(), "base.sock")
	otherSock := filepath.Join(t.TempDir(), "other.sock")
	bin := fakeSessionBinary(t, `{"sessions":[`+
		`{"default":true,"name":"default","running":true,"socket_path":"`+baseSock+`"},`+
		`{"name":"hellocare","running":true,"socket_path":"`+otherSock+`"},`+
		`{"name":"stopped","running":false,"socket_path":"/tmp/stopped.sock"}]}`)
	base := NewClient(bin, baseSock)
	sessions := NewSessions(bin, base)

	running, err := sessions.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(running) != 2 || running[0].Name != "default" || running[1].Name != "hellocare" {
		t.Fatalf("running = %+v", running)
	}
	// The base session's own name is never a prefix.
	if sessions.Prefix("default") != "" || sessions.Prefix("hellocare") != "hellocare" {
		t.Fatalf("prefixes: default=%q hellocare=%q", sessions.Prefix("default"), sessions.Prefix("hellocare"))
	}
	if client, raw := sessions.Resolve("w1:p1"); client != base || raw != "w1:p1" {
		t.Fatalf("raw id must resolve to the base client")
	}
	client, raw := sessions.Resolve("hellocare/w1:p1")
	if client == nil || client == base || raw != "w1:p1" || client.socketPath != otherSock {
		t.Fatalf("qualified id resolved to %+v / %q", client, raw)
	}
	// A session that is not running never resolves to a real client: a raw id
	// exists in every session, so the wrong server would get the command.
	if client, _ := sessions.Resolve("stopped/w1:p1"); client != nil {
		t.Fatal("a stopped session must not resolve")
	}
	if dead := sessions.ClientFor("stopped"); dead == nil || dead == base {
		t.Fatal("ClientFor must hand back an unavailable stand-in")
	}
}

func TestSessionsRefreshDropsSessionsThatWentAway(t *testing.T) {
	baseSock := filepath.Join(t.TempDir(), "base.sock")
	bin := fakeSessionBinary(t, `{"sessions":[{"default":true,"name":"default","running":true,"socket_path":"`+baseSock+`"},{"name":"gone","running":true,"socket_path":"/tmp/gone.sock"}]}`)
	sessions := NewSessions(bin, NewClient(bin, baseSock))
	if _, err := sessions.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sessions.Client("gone") == nil {
		t.Fatal("gone should be known after the first refresh")
	}
	// Second enumeration no longer lists it.
	sessions.bin = fakeSessionBinary(t, `{"sessions":[{"default":true,"name":"default","running":true,"socket_path":"`+baseSock+`"}]}`)
	sessions.base = NewClient(sessions.bin, baseSock)
	if _, err := sessions.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sessions.Client("gone") != nil {
		t.Fatal("a session that disappeared must be forgotten")
	}
}
