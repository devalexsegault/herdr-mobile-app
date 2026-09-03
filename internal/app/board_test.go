package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/board"
	"github.com/0cv/herdr-mobile-relay/internal/protocol"
)

type recordingSender struct {
	mu   sync.Mutex
	sent map[string][]map[string]any
}

func newRecordingSender() *recordingSender {
	return &recordingSender{sent: make(map[string][]map[string]any)}
}

func (r *recordingSender) SendByID(clientID string, message any) bool {
	encoded, err := json.Marshal(message)
	if err != nil {
		return false
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent[clientID] = append(r.sent[clientID], decoded)
	return true
}

func (r *recordingSender) messages(clientID string) []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]map[string]any(nil), r.sent[clientID]...)
}

func (r *recordingSender) types(clientID string) []string {
	types := []string{}
	for _, message := range r.messages(clientID) {
		messageType, _ := message["type"].(string)
		types = append(types, messageType)
	}
	return types
}

func testBoardBridge(t *testing.T, socketPath string) (*boardBridge, *recordingSender) {
	t.Helper()
	sender := newRecordingSender()
	bridge := newBoardBridge(
		board.New(socketPath, slog.New(slog.NewTextHandler(io.Discard, nil))),
		sender,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	t.Cleanup(bridge.close)
	return bridge, sender
}

func boardError(t *testing.T, message map[string]any) map[string]any {
	t.Helper()
	if ok, _ := message["ok"].(bool); ok {
		t.Fatalf("message reports success: %v", message)
	}
	failure, _ := message["error"].(map[string]any)
	if failure == nil {
		t.Fatalf("message carries no error: %v", message)
	}
	return failure
}

// The phone reaches boardd through the relay, which removes the socket's 0600
// permission boundary. The allowlist is what replaces it, so a refusal must
// come from the relay itself.
func TestBoardBridgeRefusesMethodOutsideAllowlist(t *testing.T) {
	bridge, sender := testBoardBridge(t, "/nonexistent/boardd.sock")

	bridge.handleRPC(context.Background(), "client-1", "req-1", "daemon.stop", nil)

	messages := sender.messages("client-1")
	if len(messages) != 1 {
		t.Fatalf("sent %d messages, want 1", len(messages))
	}
	failure := boardError(t, messages[0])
	if failure["kind"] != "not_allowed" {
		t.Fatalf("kind = %v, want not_allowed", failure["kind"])
	}
	if failure["code"] != float64(1) {
		t.Fatalf("code = %v, want 1", failure["code"])
	}
}

func TestBoardBridgeRefusesEmptyMethod(t *testing.T) {
	bridge, sender := testBoardBridge(t, "/nonexistent/boardd.sock")

	bridge.handleRPC(context.Background(), "client-1", "req-1", "", nil)

	failure := boardError(t, sender.messages("client-1")[0])
	if failure["kind"] != "bad_request" {
		t.Fatalf("kind = %v, want bad_request", failure["kind"])
	}
}

// Code 0 is outside boardd's documented 1..5 range on purpose: an unreachable
// daemon is not a refusal, and an app that treated it as one would show "not
// found" for a board that is merely stopped.
func TestBoardBridgeReportsAnUnreachableDaemonOutsideTheProtocolCodes(t *testing.T) {
	bridge, sender := testBoardBridge(t, "/nonexistent/boardd.sock")

	bridge.handleRPC(context.Background(), "client-1", "req-1", "board.list", nil)

	failure := boardError(t, sender.messages("client-1")[0])
	if failure["code"] != float64(0) || failure["kind"] != "unavailable" {
		t.Fatalf("error = %v, want code 0 / unavailable", failure)
	}
}

func TestBoardBridgeSubscribeAsksForAnImmediateRefetch(t *testing.T) {
	bridge, sender := testBoardBridge(t, "/nonexistent/boardd.sock")

	bridge.subscribe(context.Background(), "client-1")

	if types := sender.types("client-1"); !slices.Equal(types, []string{"board_subscribed", "board_resync"}) {
		t.Fatalf("messages = %v", types)
	}
}

func TestBoardBridgeFansOutOnlyToSubscribers(t *testing.T) {
	bridge, sender := testBoardBridge(t, "/nonexistent/boardd.sock")
	bridge.subscribe(context.Background(), "subscriber")

	bridge.forwardEvent(json.RawMessage(`{"event":"board_changed","reason":"card_moved"}`))
	bridge.forwardResync()

	if types := sender.types("subscriber"); !slices.Equal(types, []string{"board_subscribed", "board_resync", "board_event", "board_resync"}) {
		t.Fatalf("subscriber messages = %v", types)
	}
	if messages := sender.messages("bystander"); len(messages) != 0 {
		t.Fatalf("a client that never subscribed received %v", messages)
	}

	bridge.unsubscribe("subscriber")
	bridge.forwardEvent(json.RawMessage(`{"event":"board_changed"}`))
	if types := sender.types("subscriber"); len(types) != 4 {
		t.Fatalf("an unsubscribed client still received events: %v", types)
	}
}

// Events are forwarded verbatim so the app can read fields this relay was never
// taught about; the bridge must not reshape or filter them.
func TestBoardBridgeForwardsEventPayloadVerbatim(t *testing.T) {
	bridge, sender := testBoardBridge(t, "/nonexistent/boardd.sock")
	bridge.subscribe(context.Background(), "client-1")

	bridge.forwardEvent(json.RawMessage(`{"event":"run_ended","card_id":7,"run_id":3,"outcome":"ok","future_field":true}`))

	messages := sender.messages("client-1")
	event, _ := messages[len(messages)-1]["event"].(map[string]any)
	if event["outcome"] != "ok" || event["card_id"] != float64(7) || event["future_field"] != true {
		t.Fatalf("event = %v", event)
	}
}

func TestBoardDescriptorIsAbsentWithoutADaemon(t *testing.T) {
	bridge, _ := testBoardBridge(t, "/nonexistent/boardd.sock")

	if descriptor := bridge.descriptor(context.Background()); descriptor != nil {
		t.Fatalf("descriptor = %v, want nil", descriptor)
	}
}

// board_v1 is advertised from a live probe. Adding it to the static list would
// promise a board on every relay, including the ones without the plugin.
func TestBoardCapabilityIsNotAdvertisedUnconditionally(t *testing.T) {
	if slices.Contains(protocol.Capabilities, protocol.BoardCapability) {
		t.Fatal("board_v1 must not be in the unconditional capability list")
	}
}

func TestBoardAuditRecordsTheMethodButNotTheParams(t *testing.T) {
	details := auditWriteDetails(map[string]any{
		"type":       "board_rpc",
		"request_id": "req-1",
		"method":     "card.create",
		"params":     map[string]any{"title": "ship the release", "description": "secret plan"},
	})

	if details["board_method"] != "card.create" {
		t.Fatalf("board_method = %v", details["board_method"])
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret plan") {
		t.Fatalf("audit details leaked card content: %s", encoded)
	}
	if details["payload_sha256"] == nil || details["payload_bytes"] == nil {
		t.Fatalf("audit details lost the payload digest: %v", details)
	}
}

// The end-to-end path against a real daemon, skipped when none is running: the
// bridge's own allowlist, call and reply shaping over a live boardd. Read-only,
// because a developer's daemon owns real runs.
func TestBoardBridgeAgainstLiveDaemon(t *testing.T) {
	socketPath := board.DefaultSocketPath(os.Getenv("XDG_DATA_HOME"))
	if _, err := os.Stat(socketPath); err != nil {
		t.Skipf("no boardd socket at %s", socketPath)
	}
	bridge, sender := testBoardBridge(t, socketPath)

	descriptor := bridge.descriptor(context.Background())
	if descriptor == nil {
		// A socket file can outlive its daemon, or the daemon may be
		// restarting; neither is this code's fault, so the live check steps
		// aside rather than failing a release gate on local daemon state.
		t.Skipf("boardd socket at %s is not answering", socketPath)
	}
	if descriptor["version"] == "" || descriptor["methods"] == nil {
		t.Fatalf("descriptor = %v", descriptor)
	}

	bridge.handleRPC(context.Background(), "client-1", "req-1", "board.list", nil)
	message := sender.messages("client-1")[0]
	if ok, _ := message["ok"].(bool); !ok {
		t.Fatalf("board.list failed against the live daemon: %v", message)
	}
	if message["result"] == nil {
		t.Fatalf("board.list returned no result: %v", message)
	}

	bridge.handleRPC(context.Background(), "client-1", "req-2", "daemon.stop", nil)
	failure := boardError(t, sender.messages("client-1")[1])
	if failure["kind"] != "not_allowed" {
		t.Fatalf("a live daemon must not make daemon.stop reachable: %v", failure)
	}
}

func TestBoardBridgeCloseIsIdempotent(t *testing.T) {
	bridge, _ := testBoardBridge(t, "/nonexistent/boardd.sock")
	bridge.subscribe(context.Background(), "client-1")

	done := make(chan struct{})
	go func() {
		bridge.close()
		bridge.close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("close did not return")
	}
}
