package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"

	"github.com/0cv/herdr-mobile-relay/internal/board"
)

// boardBridge exposes a local herdr-board daemon to paired phones.
//
// It is deliberately thin. Rather than mirroring boardd's forty protocol
// methods as forty relay messages — forty places to update every time the board
// plugin ships a field — the bridge proxies one `board_rpc` envelope against an
// allowlist and forwards results untouched. What the relay does own is the part
// boardd cannot: deciding which methods a remote phone may reach at all,
// auditing the mutations, and fanning the daemon's event stream out to the
// subscribed clients.
type boardBridge struct {
	client *board.Client
	sender boardSender
	logger *slog.Logger

	mu      sync.Mutex
	subs    map[string]bool
	started bool
}

// boardSender is the slice of the hub the bridge needs. Addressing clients by
// id rather than by connection keeps the bridge free of transport types: the
// event fan-out already has nothing but ids to work from.
type boardSender interface {
	SendByID(clientID string, message any) bool
}

func newBoardBridge(client *board.Client, sender boardSender, logger *slog.Logger) *boardBridge {
	bridge := &boardBridge{client: client, sender: sender, logger: logger, subs: make(map[string]bool)}
	client.SetHandlers(bridge.forwardEvent, bridge.forwardResync)
	return bridge
}

// status probes the daemon for the connect handshake. Anything other than a
// clean answer withholds the capability, so an uninstalled or stopped board
// leaves no board UI on the phone to explain away.
func (b *boardBridge) status(ctx context.Context) (board.Status, bool) {
	if b == nil || b.client == nil {
		return board.Status{}, false
	}
	return b.client.Status(ctx)
}

// descriptor is the board section of the connect handshake. herdr_connected is
// carried through because boardd requires an exact Herdr version: a board that
// is running but cannot reach Herdr accepts reads and refuses every dispatch,
// and the app has to be able to say so rather than surfacing bare code-4
// failures on each attempt.
func (b *boardBridge) descriptor(ctx context.Context) map[string]any {
	status, ok := b.status(ctx)
	if !ok {
		return nil
	}
	return map[string]any{
		"version":         status.Version,
		"herdr_connected": status.HerdrConnected,
		"active_runs":     status.ActiveRuns,
		"queued_runs":     status.QueuedRuns,
		"methods":         board.AllowedMethods(),
	}
}

func (b *boardBridge) handleRPC(ctx context.Context, clientID, requestID, method string, params json.RawMessage) {
	if b == nil || b.client == nil {
		return
	}
	if method == "" {
		b.sendError(clientID, requestID, method, 1, "bad_request", "A board method is required")
		return
	}
	if !board.Allowed(method) {
		// Refused by the relay, not by boardd: say so with the daemon's own
		// "bad request" code so the app has one error shape to handle.
		b.sendError(clientID, requestID, method, 1, "not_allowed", "This board method is not available through the relay")
		return
	}

	result, err := b.client.Call(ctx, method, params)
	if err == nil {
		b.sender.SendByID(clientID, map[string]any{
			"type":       "board_result",
			"request_id": requestID,
			"method":     method,
			"ok":         true,
			"result":     result,
		})
		return
	}

	var protocolErr *board.Error
	if errors.As(err, &protocolErr) {
		b.sender.SendByID(clientID, map[string]any{
			"type":       "board_result",
			"request_id": requestID,
			"method":     method,
			"ok":         false,
			"error":      protocolErr,
		})
		return
	}
	// Code 0 is outside boardd's 1..5 range on purpose: it means the daemon
	// never answered, so the app must not read it as a refusal.
	b.logger.Warn("board request failed", "method", method, "error", err)
	b.sendError(clientID, requestID, method, 0, "unavailable", "The board daemon is unavailable")
}

func (b *boardBridge) sendError(clientID, requestID, method string, code int, kind, message string) {
	b.sender.SendByID(clientID, map[string]any{
		"type":       "board_result",
		"request_id": requestID,
		"method":     method,
		"ok":         false,
		"error":      map[string]any{"code": code, "kind": kind, "message": message},
	})
}

// subscribe adds a client to the event fan-out and starts the upstream stream
// on first use. The immediate resync is not a formality: the app has no board
// state yet, and the same message is what it will receive after any later
// reconnect, so both paths share one code path on the client.
func (b *boardBridge) subscribe(ctx context.Context, clientID string) {
	if b == nil || b.client == nil {
		return
	}
	b.mu.Lock()
	b.subs[clientID] = true
	start := !b.started
	b.started = true
	b.mu.Unlock()

	if start {
		b.client.StartEvents(ctx)
	}
	b.sender.SendByID(clientID, map[string]any{"type": "board_subscribed", "ok": true})
	b.sender.SendByID(clientID, map[string]any{"type": "board_resync", "reason": "subscribed"})
}

func (b *boardBridge) unsubscribe(clientID string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	delete(b.subs, clientID)
	b.mu.Unlock()
}

// forwardEvent relays one daemon event verbatim. boardd's events are coarse by
// contract — the payload is for logs and toasts, and the client is expected to
// refetch — so the bridge neither interprets them nor lets an unknown field
// stop it forwarding.
func (b *boardBridge) forwardEvent(line json.RawMessage) {
	b.each(func(clientID string) {
		b.sender.SendByID(clientID, map[string]any{"type": "board_event", "event": line})
	})
}

// forwardResync tells subscribers the event stream had a gap. boardd
// disconnects a subscriber it cannot deliver to, and reconnecting does not
// replay what was missed, so a refetch is the only correct response.
func (b *boardBridge) forwardResync() {
	b.each(func(clientID string) {
		b.sender.SendByID(clientID, map[string]any{"type": "board_resync", "reason": "reconnected"})
	})
}

func (b *boardBridge) each(send func(clientID string)) {
	b.mu.Lock()
	clientIDs := make([]string, 0, len(b.subs))
	for clientID := range b.subs {
		clientIDs = append(clientIDs, clientID)
	}
	b.mu.Unlock()
	for _, clientID := range clientIDs {
		send(clientID)
	}
}

func (b *boardBridge) close() {
	if b == nil || b.client == nil {
		return
	}
	b.client.Close()
}
