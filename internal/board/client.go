// Package board speaks the herdr-board daemon's socket protocol v1 so the relay
// can bridge a paired phone to a board running on this machine.
//
// boardd listens on a Unix socket and exchanges newline-delimited JSON:
// requests are {"id","method","params"}, replies are {"id","result"} or
// {"id","error":{code,message,kind?,details?}}, and a connection that sends
// events.subscribe additionally receives id-less event objects until it closes.
// One connection carries both directions at once, so this client multiplexes
// concurrent requests over a single subscribed connection.
//
// Two contract details shape the design:
//
//   - Events are coarse by design. Their payload is documented as being for
//     logs and toasts; the client is expected to refetch. This client therefore
//     forwards event lines verbatim and never tries to derive state from them.
//   - boardd disconnects a subscriber it cannot deliver to. A dropped
//     connection means events were missed, so every reconnect raises a resync
//     signal rather than silently resuming.
package board

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// requestTimeout bounds a single RPC. boardd's own slowest documented path
	// is session.list, which it caps at ten seconds by killing the herdr CLI it
	// shells out to; this leaves room for that answer to come back as a proper
	// protocol error instead of being cut off as a transport failure.
	requestTimeout = 12 * time.Second
	dialTimeout    = 2 * time.Second
	// statusTimeout bounds the capability probe. It runs on the WebSocket
	// connect path, where a missing daemon must not delay the handshake.
	statusTimeout   = 750 * time.Millisecond
	statusTTL       = 5 * time.Second
	readBufferBytes = 64 * 1024
	maxLineBytes    = 8 * 1024 * 1024
	minRedialDelay  = 500 * time.Millisecond
	maxRedialDelay  = 30 * time.Second
)

// ErrUnavailable reports that boardd could not be reached at all: no socket, a
// refused connection, or a daemon that went away mid-request. It is distinct
// from an *Error, which proves boardd answered.
var ErrUnavailable = errors.New("board: boardd is unavailable")

// ErrMethodNotAllowed reports a method outside the bridge allowlist. It never
// reaches the socket.
var ErrMethodNotAllowed = errors.New("board: method is not available through the relay")

// Error is a boardd protocol error answered on the wire. Code, Kind and Details
// are preserved verbatim: the app distinguishes "not found" (2) from "invalid
// state" (3) and "herdr unavailable" (4), and flattening them into a string
// would leave it guessing.
type Error struct {
	Code    int             `json:"code"`
	Kind    string          `json:"kind,omitempty"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return "board: request failed"
	}
	if e.Message == "" {
		return fmt.Sprintf("board: error %d", e.Code)
	}
	return e.Message
}

// Status is the daemon.status result. DBPath is deliberately not forwarded to
// the app by the bridge; it is kept here for logs.
type Status struct {
	Version        string `json:"version"`
	DBPath         string `json:"db_path"`
	HerdrConnected bool   `json:"herdr_connected"`
	ActiveRuns     int    `json:"active_runs"`
	QueuedRuns     int    `json:"queued_runs"`
}

// DefaultSocketPath resolves the socket exactly as boardd and its own clients
// do: $BOARD_SOCKET wins, otherwise <data>/herdr-board/boardd.sock. dataHome is
// the caller's already-resolved XDG data directory.
func DefaultSocketPath(dataHome string) string {
	if path := os.Getenv("BOARD_SOCKET"); path != "" {
		return path
	}
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "herdr-board", "boardd.sock")
}

// Client is a lazily connected, automatically reconnecting boardd client. The
// zero value is not usable; call New.
type Client struct {
	path   string
	logger *slog.Logger

	mu            sync.Mutex
	conn          *connection
	everConnected bool
	closed        bool

	handlerMu sync.RWMutex
	onEvent   func(json.RawMessage)
	onResync  func()

	statusMu   sync.Mutex
	statusAt   time.Time
	statusVal  Status
	statusOK   bool
	supervised bool

	stopSupervisor context.CancelFunc
	supervisorDone chan struct{}
}

func New(path string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{path: path, logger: logger}
}

// SetHandlers installs the event and resync callbacks. Both are invoked from
// the client's own goroutines and must not block: a slow handler backs up the
// read loop, and boardd disconnects a subscriber it cannot deliver to.
func (c *Client) SetHandlers(onEvent func(json.RawMessage), onResync func()) {
	c.handlerMu.Lock()
	c.onEvent, c.onResync = onEvent, onResync
	c.handlerMu.Unlock()
}

// StartEvents keeps a subscribed connection alive so events flow even while the
// app sends no requests, redialing with backoff for as long as ctx lives. It is
// idempotent: the second and later calls do nothing.
func (c *Client) StartEvents(ctx context.Context) {
	c.mu.Lock()
	if c.supervised || c.closed {
		c.mu.Unlock()
		return
	}
	c.supervised = true
	supervisorCtx, cancel := context.WithCancel(ctx)
	c.stopSupervisor = cancel
	c.supervisorDone = make(chan struct{})
	done := c.supervisorDone
	c.mu.Unlock()

	go func() {
		defer close(done)
		c.superviseEvents(supervisorCtx)
	}()
}

func (c *Client) superviseEvents(ctx context.Context) {
	delay := minRedialDelay
	for ctx.Err() == nil {
		conn, err := c.acquire(ctx)
		if err != nil {
			// A board that is simply not installed is the common case, so this
			// stays at debug: the capability is already withheld, and the app
			// shows no board UI to explain.
			c.logger.Debug("board daemon is unreachable", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			if delay < maxRedialDelay {
				delay *= 2
			}
			continue
		}
		delay = minRedialDelay
		select {
		case <-ctx.Done():
			return
		case <-conn.done:
		}
	}
}

// Call sends one allowlisted request and returns its raw result. A transport
// failure is retried once on a fresh connection, which is safe for reads and
// for boardd's idempotent-by-id mutations only insofar as the first attempt
// never reached the socket; an attempt that did write is not retried.
func (c *Client) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	if !Allowed(method) {
		return nil, fmt.Errorf("%w: %s", ErrMethodNotAllowed, method)
	}
	return c.call(ctx, method, params)
}

func (c *Client) call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	callCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, requestTimeout)
		defer cancel()
	}

	var lastErr error
	for attempt := range 2 {
		conn, err := c.acquire(callCtx)
		if err != nil {
			return nil, err
		}
		result, wrote, err := conn.request(callCtx, method, params)
		if err == nil {
			return result, nil
		}
		var protocolErr *Error
		if errors.As(err, &protocolErr) {
			return nil, err
		}
		conn.shutdown(err)
		c.forget(conn)
		// Bytes on the wire mean boardd may have executed the request. Retrying
		// would risk a duplicate card or a second dispatch, so the caller is
		// told the outcome is unknown instead.
		if wrote || callCtx.Err() != nil || attempt == 1 {
			lastErr = err
			break
		}
		lastErr = err
	}
	if errors.Is(lastErr, context.DeadlineExceeded) || errors.Is(lastErr, context.Canceled) {
		return nil, lastErr
	}
	return nil, fmt.Errorf("%w: %v", ErrUnavailable, lastErr)
}

// Status returns the daemon status, cached briefly because it is the probe
// behind the board_v1 capability and every app connection asks for it.
func (c *Client) Status(ctx context.Context) (Status, bool) {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	if time.Since(c.statusAt) < statusTTL {
		return c.statusVal, c.statusOK
	}

	probeCtx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	raw, err := c.call(probeCtx, "daemon.status", nil)
	c.statusAt = time.Now()
	if err != nil {
		c.statusVal, c.statusOK = Status{}, false
		return c.statusVal, false
	}
	var status Status
	if err := json.Unmarshal(raw, &status); err != nil {
		c.statusVal, c.statusOK = Status{}, false
		return c.statusVal, false
	}
	c.statusVal, c.statusOK = status, true
	return status, true
}

// Available reports whether boardd answered a status probe.
func (c *Client) Available(ctx context.Context) bool {
	_, ok := c.Status(ctx)
	return ok
}

// Close stops the event supervisor and drops the connection.
func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	conn := c.conn
	c.conn = nil
	stop := c.stopSupervisor
	done := c.supervisorDone
	c.mu.Unlock()

	if stop != nil {
		stop()
	}
	if conn != nil {
		conn.shutdown(errors.New("board: client closed"))
	}
	if done != nil {
		<-done
	}
}

// acquire returns the live connection, dialing and subscribing if needed. A
// dial that replaces an earlier connection raises the resync signal: the gap
// between the two connections may have swallowed events, and the contract puts
// the burden of refetching on the client.
func (c *Client) acquire(ctx context.Context) (*connection, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrUnavailable
	}
	if c.conn != nil && !c.conn.isClosed() {
		conn := c.conn
		c.mu.Unlock()
		return conn, nil
	}
	c.conn = nil
	path := c.path
	reconnected := c.everConnected
	c.mu.Unlock()

	if path == "" {
		return nil, fmt.Errorf("%w: no socket path", ErrUnavailable)
	}
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	netConn, err := (&net.Dialer{}).DialContext(dialCtx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	conn := newConnection(netConn, c.dispatchEvent)
	if err := conn.subscribe(ctx); err != nil {
		conn.shutdown(err)
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		conn.shutdown(errors.New("board: client closed"))
		return nil, ErrUnavailable
	}
	// Another goroutine may have won the race; keep whichever connection is
	// already published so requests and events share one subscription.
	if c.conn != nil && !c.conn.isClosed() {
		existing := c.conn
		c.mu.Unlock()
		conn.shutdown(errors.New("board: duplicate connection"))
		return existing, nil
	}
	c.conn = conn
	c.everConnected = true
	c.mu.Unlock()

	if reconnected {
		c.dispatchResync()
	}
	return conn, nil
}

func (c *Client) forget(conn *connection) {
	c.mu.Lock()
	if c.conn == conn {
		c.conn = nil
	}
	c.mu.Unlock()
}

func (c *Client) dispatchEvent(line json.RawMessage) {
	c.handlerMu.RLock()
	handler := c.onEvent
	c.handlerMu.RUnlock()
	if handler != nil {
		handler(line)
	}
}

func (c *Client) dispatchResync() {
	c.handlerMu.RLock()
	handler := c.onResync
	c.handlerMu.RUnlock()
	if handler != nil {
		handler()
	}
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

// connection owns one socket: its read loop, its pending requests, and the
// subscription established on it. It never reconnects — Client does that by
// replacing the whole connection, so a request can never be answered by a
// socket other than the one it was written to.
type connection struct {
	conn    net.Conn
	reader  *bufio.Reader
	onEvent func(json.RawMessage)

	writeMu sync.Mutex

	mu      sync.Mutex
	pending map[string]chan rpcResult
	seq     uint64
	closed  bool
	err     error

	done chan struct{}
}

func newConnection(netConn net.Conn, onEvent func(json.RawMessage)) *connection {
	conn := &connection{
		conn:    netConn,
		reader:  bufio.NewReaderSize(netConn, readBufferBytes),
		onEvent: onEvent,
		pending: make(map[string]chan rpcResult),
		done:    make(chan struct{}),
	}
	go conn.readLoop()
	return conn
}

// subscribe turns this connection into an event stream. It is a normal request,
// answered on the same connection, so it goes through the ordinary path.
func (c *connection) subscribe(ctx context.Context) error {
	subscribeCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	_, _, err := c.request(subscribeCtx, "events.subscribe", nil)
	return err
}

// request writes one request and waits for its reply. The bool reports whether
// request bytes reached the socket, which is the caller's retry-safety
// boundary: once written, boardd may have executed the request even if no reply
// comes back.
func (c *connection) request(
	ctx context.Context,
	method string,
	params json.RawMessage,
) (json.RawMessage, bool, error) {
	c.mu.Lock()
	if c.closed {
		err := c.err
		c.mu.Unlock()
		if err == nil {
			err = errors.New("board: connection is closed")
		}
		return nil, false, err
	}
	c.seq++
	id := fmt.Sprintf("relay-%d", c.seq)
	reply := make(chan rpcResult, 1)
	c.pending[id] = reply
	c.mu.Unlock()

	// The protocol doc says params may be omitted, but boardd deserializes the
	// member into each method's own params struct, and several of them —
	// space.list among them — reject a null where a struct is expected. An
	// explicit empty object is what "omitted = {}" actually means on the wire.
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	payload := map[string]any{"id": id, "method": method, "params": params}
	line, err := json.Marshal(payload)
	if err != nil {
		c.resolve(id, rpcResult{err: fmt.Errorf("encode board request: %w", err)})
		return nil, false, fmt.Errorf("encode board request: %w", err)
	}
	line = append(line, '\n')

	c.writeMu.Lock()
	deadline := time.Now().Add(dialTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = c.conn.SetWriteDeadline(deadline)
	written, writeErr := c.conn.Write(line)
	_ = c.conn.SetWriteDeadline(time.Time{})
	c.writeMu.Unlock()

	if writeErr != nil {
		c.resolve(id, rpcResult{err: writeErr})
		return nil, written > 0, fmt.Errorf("write board request: %w", writeErr)
	}

	select {
	case answer := <-reply:
		return answer.result, true, answer.err
	case <-ctx.Done():
		c.resolve(id, rpcResult{err: ctx.Err()})
		return nil, true, ctx.Err()
	case <-c.done:
		c.mu.Lock()
		err := c.err
		c.mu.Unlock()
		if err == nil {
			err = errors.New("board: connection closed before a reply")
		}
		return nil, true, err
	}
}

func (c *connection) readLoop() {
	for {
		line, err := readLine(c.reader)
		if err != nil {
			c.shutdown(err)
			return
		}
		var envelope struct {
			ID     string          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *Error          `json:"error"`
			Event  string          `json:"event"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			// One malformed line is not a reason to tear down a healthy
			// subscription: skip it and keep reading.
			continue
		}
		if envelope.ID == "" {
			if envelope.Event != "" && c.onEvent != nil {
				c.onEvent(append(json.RawMessage(nil), line...))
			}
			continue
		}
		answer := rpcResult{result: envelope.Result}
		if envelope.Error != nil {
			answer = rpcResult{err: envelope.Error}
		}
		c.resolve(envelope.ID, answer)
	}
}

func (c *connection) resolve(id string, answer rpcResult) {
	c.mu.Lock()
	reply, ok := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if ok {
		reply <- answer
	}
}

// shutdown closes the socket and fails every in-flight request with cause, so
// no caller is left waiting on a connection that will never answer.
func (c *connection) shutdown(cause error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	if cause == nil {
		cause = errors.New("board: connection closed")
	}
	c.err = cause
	pending := c.pending
	c.pending = make(map[string]chan rpcResult)
	c.mu.Unlock()

	_ = c.conn.Close()
	for _, reply := range pending {
		reply <- rpcResult{err: cause}
	}
	close(c.done)
}

func (c *connection) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// readLine reads one NDJSON line, growing past the buffered reader's window for
// a large board.get payload but refusing a line that would let a misbehaving
// peer exhaust memory.
func readLine(reader *bufio.Reader) ([]byte, error) {
	line := make([]byte, 0, readBufferBytes)
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(line)+len(fragment) > maxLineBytes {
			return nil, fmt.Errorf("board response exceeds %d bytes", maxLineBytes)
		}
		line = append(line, fragment...)
		if err == nil {
			return line, nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return nil, err
		}
	}
}
