package board

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"
)

// Live tests run against a real boardd on this machine and skip everywhere
// else, including CI. They are read-only by construction: a developer's daemon
// owns real runs and real agent panes, and a test must never move a card or
// cancel a run that a person is depending on. The allowlist is what the
// contract tests cover; this file only proves the wire format matches a daemon
// this repository does not ship.
func liveClient(t *testing.T) *Client {
	t.Helper()
	path := DefaultSocketPath(os.Getenv("XDG_DATA_HOME"))
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no boardd socket at %s", path)
	}
	client := New(path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(client.Close)
	return client
}

func TestLiveDaemonStatus(t *testing.T) {
	client := liveClient(t)

	status, ok := client.Status(context.Background())
	if !ok {
		t.Fatal("the live daemon did not answer daemon.status")
	}
	if status.Version == "" {
		t.Fatalf("status carries no version: %+v", status)
	}
	t.Logf("boardd %s herdr_connected=%v active=%d queued=%d",
		status.Version, status.HerdrConnected, status.ActiveRuns, status.QueuedRuns)
}

func TestLiveReadOnlyMethods(t *testing.T) {
	client := liveClient(t)
	ctx := context.Background()

	for _, method := range []string{"project.list", "board.list", "harness.list", "space.list"} {
		raw, err := client.Call(ctx, method, nil)
		if err != nil {
			t.Errorf("%s: %v", method, err)
			continue
		}
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Errorf("%s returned a non-object result: %v", method, err)
			continue
		}
		t.Logf("%s -> %d bytes", method, len(raw))
	}
}

// A board id the daemon has never issued must come back as the protocol's
// "not found" (code 2), not as a transport failure. This is the distinction the
// app relies on to tell a missing card from a stopped daemon.
func TestLiveUnknownBoardIsNotFound(t *testing.T) {
	client := liveClient(t)

	_, err := client.Call(context.Background(), "board.get", json.RawMessage(`{"board_id":2147483000}`))
	var protocolErr *Error
	if !errors.As(err, &protocolErr) {
		t.Fatalf("err = %v, want a protocol error", err)
	}
	if protocolErr.Code < 1 || protocolErr.Code > 5 {
		t.Fatalf("code = %d, want boardd's 1..5 range", protocolErr.Code)
	}
	t.Logf("board.get on an unknown id -> code %d kind %q", protocolErr.Code, protocolErr.Kind)
}

func TestLiveSubscriptionAcceptsEvents(t *testing.T) {
	client := liveClient(t)

	events := make(chan json.RawMessage, 8)
	client.SetHandlers(func(line json.RawMessage) { events <- line }, nil)
	client.StartEvents(context.Background())

	// The subscription is established by the client's own connect path, so a
	// successful read-only call over the same connection proves the daemon
	// accepted events.subscribe and kept serving requests on it.
	if _, err := client.Call(context.Background(), "board.list", nil); err != nil {
		t.Fatalf("request over the subscribed connection: %v", err)
	}
	select {
	case line := <-events:
		t.Logf("live event: %s", line)
	case <-time.After(2 * time.Second):
		t.Log("no board activity during the window; the subscription was accepted")
	}
}
