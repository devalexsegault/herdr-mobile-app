package board

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testClient(t *testing.T, path string) *Client {
	t.Helper()
	client := New(path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(client.Close)
	return client
}

func TestDefaultSocketPathPrefersBoardSocketEnv(t *testing.T) {
	t.Setenv("BOARD_SOCKET", "/run/custom/boardd.sock")
	if got := DefaultSocketPath("/home/example/.local/share"); got != "/run/custom/boardd.sock" {
		t.Fatalf("DefaultSocketPath = %q", got)
	}
}

func TestDefaultSocketPathFallsBackToDataHome(t *testing.T) {
	t.Setenv("BOARD_SOCKET", "")
	want := filepath.Join("/home/example/.local/share", "herdr-board", "boardd.sock")
	if got := DefaultSocketPath("/home/example/.local/share"); got != want {
		t.Fatalf("DefaultSocketPath = %q, want %q", got, want)
	}
}

func TestCallReturnsResult(t *testing.T) {
	daemon := newFakeDaemon(t)
	daemon.setHandler(func(method string, params json.RawMessage) (any, *Error) {
		if method != "board.get" {
			t.Errorf("unexpected method %q", method)
		}
		return map[string]any{"board_id": 7, "columns": []any{}}, nil
	})
	client := testClient(t, daemon.path)

	raw, err := client.Call(context.Background(), "board.get", json.RawMessage(`{"board_id":7}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var result struct {
		BoardID int `json:"board_id"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.BoardID != 7 {
		t.Fatalf("board_id = %d", result.BoardID)
	}
}

// The relay opens one subscribed connection and multiplexes every request over
// it, so a reply must reach the caller that asked for it even when the daemon
// answers out of order.
func TestCallCorrelatesConcurrentRequestsOnOneConnection(t *testing.T) {
	daemon := newFakeDaemon(t)
	release := make(chan struct{})
	daemon.setHandler(func(method string, params json.RawMessage) (any, *Error) {
		if method == "card.get" {
			<-release
		}
		return map[string]any{"method": method}, nil
	})
	client := testClient(t, daemon.path)

	var wg sync.WaitGroup
	results := make([]string, 2)
	errs := make([]error, 2)
	for index, method := range []string{"card.get", "board.list"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, err := client.Call(context.Background(), method, nil)
			if err != nil {
				errs[index] = err
				return
			}
			var answer struct {
				Method string `json:"method"`
			}
			if err := json.Unmarshal(raw, &answer); err != nil {
				errs[index] = err
				return
			}
			results[index] = answer.Method
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	for index, err := range errs {
		if err != nil {
			t.Fatalf("call %d: %v", index, err)
		}
	}
	if results[0] != "card.get" || results[1] != "board.list" {
		t.Fatalf("replies were mismatched: %#v", results)
	}
}

// boardd deserializes params into each method's own struct and rejects a null
// where a struct is expected — space.list does exactly that on a real daemon —
// so a call with no params has to put an empty object on the wire.
func TestCallSendsAnExplicitEmptyParamsObject(t *testing.T) {
	daemon := newFakeDaemon(t)
	daemon.setHandler(func(method string, params json.RawMessage) (any, *Error) {
		return map[string]any{"spaces": []any{}}, nil
	})
	client := testClient(t, daemon.path)

	if _, err := client.Call(context.Background(), "space.list", nil); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got := daemon.paramsFor("space.list"); got != "{}" {
		t.Fatalf("params on the wire = %q, want {}", got)
	}
}

func TestCallPreservesProtocolErrorEnvelope(t *testing.T) {
	daemon := newFakeDaemon(t)
	daemon.setHandler(func(method string, params json.RawMessage) (any, *Error) {
		return nil, &Error{Code: 2, Kind: "not_found", Message: "card 41 does not exist", Details: json.RawMessage(`{"card_id":41}`)}
	})
	client := testClient(t, daemon.path)

	_, err := client.Call(context.Background(), "card.get", json.RawMessage(`{"card_id":41}`))
	var protocolErr *Error
	if !errors.As(err, &protocolErr) {
		t.Fatalf("err = %v, want *Error", err)
	}
	if protocolErr.Code != 2 || protocolErr.Kind != "not_found" {
		t.Fatalf("error envelope lost fields: %+v", protocolErr)
	}
	if string(protocolErr.Details) != `{"card_id":41}` {
		t.Fatalf("details = %s", protocolErr.Details)
	}
	if errors.Is(err, ErrUnavailable) {
		t.Fatal("an answered protocol error must not be reported as unavailable")
	}
}

// A method outside the allowlist must be refused by the relay itself, never
// forwarded: the socket's 0600 permissions no longer protect it once a phone
// can reach it.
func TestCallRefusesMethodOutsideAllowlistWithoutReachingDaemon(t *testing.T) {
	daemon := newFakeDaemon(t)
	client := testClient(t, daemon.path)

	_, err := client.Call(context.Background(), "daemon.stop", nil)
	if !errors.Is(err, ErrMethodNotAllowed) {
		t.Fatalf("err = %v, want ErrMethodNotAllowed", err)
	}
	if methods := daemon.methods(); len(methods) != 0 {
		t.Fatalf("daemon saw %v, want no traffic", methods)
	}
}

func TestCallOnMissingDaemonIsUnavailable(t *testing.T) {
	client := testClient(t, filepath.Join(t.TempDir(), "absent.sock"))

	_, err := client.Call(context.Background(), "board.list", nil)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if client.Available(context.Background()) {
		t.Fatal("Available must be false without a daemon")
	}
}

func TestStatusReportsDaemon(t *testing.T) {
	daemon := newFakeDaemon(t)
	daemon.setHandler(func(method string, params json.RawMessage) (any, *Error) {
		return map[string]any{
			"version": "0.16.1", "db_path": "/tmp/board.db",
			"herdr_connected": true, "active_runs": 2, "queued_runs": 1,
		}, nil
	})
	client := testClient(t, daemon.path)

	status, ok := client.Status(context.Background())
	if !ok {
		t.Fatal("Status reported the daemon as unavailable")
	}
	if status.Version != "0.16.1" || !status.HerdrConnected || status.ActiveRuns != 2 || status.QueuedRuns != 1 {
		t.Fatalf("status = %+v", status)
	}
}

func TestEventsReachHandlerVerbatim(t *testing.T) {
	daemon := newFakeDaemon(t)
	client := testClient(t, daemon.path)

	events := make(chan json.RawMessage, 4)
	client.SetHandlers(func(line json.RawMessage) { events <- line }, nil)
	client.StartEvents(context.Background())
	waitForSubscribe(t, daemon)

	daemon.emit(map[string]any{"event": "board_changed", "reason": "card_moved", "board_id": 3, "column_id": 9})

	select {
	case line := <-events:
		var event struct {
			Event    string `json:"event"`
			Reason   string `json:"reason"`
			ColumnID int    `json:"column_id"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		// Forwarding the line verbatim is what lets the app read fields this
		// relay was never taught about.
		if event.Event != "board_changed" || event.Reason != "card_moved" || event.ColumnID != 9 {
			t.Fatalf("event = %+v", event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event was delivered")
	}
}

// boardd disconnects a subscriber it cannot deliver to, and the contract makes
// refetching the client's job. The bridge must therefore learn about every
// reconnect rather than silently resuming a stream with a hole in it.
func TestReconnectRaisesResync(t *testing.T) {
	daemon := newFakeDaemon(t)
	client := testClient(t, daemon.path)

	resyncs := make(chan struct{}, 4)
	client.SetHandlers(nil, func() { resyncs <- struct{}{} })
	client.StartEvents(context.Background())
	waitForSubscribe(t, daemon)

	select {
	case <-resyncs:
		t.Fatal("the first connection must not raise a resync")
	case <-time.After(100 * time.Millisecond):
	}

	daemon.dropConnections()

	select {
	case <-resyncs:
	case <-time.After(5 * time.Second):
		t.Fatal("no resync was raised after the daemon dropped the connection")
	}
	waitForSubscribe(t, daemon)
}

func TestCallAfterDaemonDropIsRetriedOnAFreshConnection(t *testing.T) {
	daemon := newFakeDaemon(t)
	daemon.setHandler(func(method string, params json.RawMessage) (any, *Error) {
		return map[string]any{"ok": true}, nil
	})
	client := testClient(t, daemon.path)

	if _, err := client.Call(context.Background(), "board.list", nil); err != nil {
		t.Fatalf("first call: %v", err)
	}
	daemon.dropConnections()

	if _, err := client.Call(context.Background(), "board.list", nil); err != nil {
		t.Fatalf("call after drop: %v", err)
	}
}

func TestCloseStopsTheEventSupervisor(t *testing.T) {
	daemon := newFakeDaemon(t)
	client := New(daemon.path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	client.StartEvents(context.Background())
	waitForSubscribe(t, daemon)

	done := make(chan struct{})
	go func() {
		client.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not return")
	}
}

func waitForSubscribe(t *testing.T, daemon *fakeDaemon) {
	t.Helper()
	select {
	case <-daemon.subscribeCh:
	case <-time.After(3 * time.Second):
		t.Fatal("the client never subscribed")
	}
}
