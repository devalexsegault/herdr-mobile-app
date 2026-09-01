package board

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"sync"
	"testing"
)

// fakeDaemon is a boardd stand-in: a Unix socket speaking the same NDJSON
// envelope. The relay must work against a daemon it does not ship, so every
// test drives the real wire format rather than a mocked client.
type fakeDaemon struct {
	t    *testing.T
	path string
	ln   net.Listener

	mu         sync.Mutex
	conns      []net.Conn
	subscribed []net.Conn
	calls      []call

	handler func(method string, params json.RawMessage) (any, *Error)

	subscribeCh chan struct{}
}

type call struct {
	method string
	params json.RawMessage
}

func newFakeDaemon(t *testing.T) *fakeDaemon {
	t.Helper()
	path := filepath.Join(t.TempDir(), "boardd.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	daemon := &fakeDaemon{t: t, path: path, ln: ln, subscribeCh: make(chan struct{}, 8)}
	t.Cleanup(daemon.stop)
	go daemon.accept()
	return daemon
}

func (d *fakeDaemon) accept() {
	for {
		conn, err := d.ln.Accept()
		if err != nil {
			return
		}
		d.mu.Lock()
		d.conns = append(d.conns, conn)
		d.mu.Unlock()
		go d.serve(conn)
	}
}

func (d *fakeDaemon) serve(conn net.Conn) {
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var request struct {
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(line, &request); err != nil {
			return
		}
		d.mu.Lock()
		d.calls = append(d.calls, call{method: request.Method, params: request.Params})
		handler := d.handler
		d.mu.Unlock()

		if request.Method == "events.subscribe" {
			d.mu.Lock()
			d.subscribed = append(d.subscribed, conn)
			d.mu.Unlock()
			d.reply(conn, request.ID, map[string]any{"subscribed": true}, nil)
			select {
			case d.subscribeCh <- struct{}{}:
			default:
			}
			continue
		}
		if handler == nil {
			d.reply(conn, request.ID, map[string]any{"ok": true}, nil)
			continue
		}
		result, protocolErr := handler(request.Method, request.Params)
		d.reply(conn, request.ID, result, protocolErr)
	}
}

func (d *fakeDaemon) reply(conn net.Conn, id string, result any, protocolErr *Error) {
	envelope := map[string]any{"id": id}
	if protocolErr != nil {
		envelope["error"] = protocolErr
	} else {
		envelope["result"] = result
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		d.t.Errorf("marshal reply: %v", err)
		return
	}
	d.write(conn, payload)
}

func (d *fakeDaemon) write(conn net.Conn, payload []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, _ = conn.Write(append(payload, '\n'))
}

// emit pushes one id-less event line to every subscribed connection.
func (d *fakeDaemon) emit(event map[string]any) {
	payload, err := json.Marshal(event)
	if err != nil {
		d.t.Fatalf("marshal event: %v", err)
	}
	d.mu.Lock()
	conns := append([]net.Conn(nil), d.subscribed...)
	d.mu.Unlock()
	for _, conn := range conns {
		d.write(conn, payload)
	}
}

// dropConnections closes every accepted connection, simulating the disconnect
// boardd performs on a subscriber it cannot deliver to.
func (d *fakeDaemon) dropConnections() {
	d.mu.Lock()
	conns := append([]net.Conn(nil), d.conns...)
	d.conns = nil
	d.subscribed = nil
	d.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close()
	}
}

func (d *fakeDaemon) setHandler(handler func(method string, params json.RawMessage) (any, *Error)) {
	d.mu.Lock()
	d.handler = handler
	d.mu.Unlock()
}

func (d *fakeDaemon) methods() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	methods := make([]string, 0, len(d.calls))
	for _, recorded := range d.calls {
		methods = append(methods, recorded.method)
	}
	return methods
}

// paramsFor returns the params member as it arrived on the wire for the first
// call to method, so a test can assert what the client actually sent.
func (d *fakeDaemon) paramsFor(method string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, recorded := range d.calls {
		if recorded.method == method {
			return string(recorded.params)
		}
	}
	return ""
}

func (d *fakeDaemon) stop() {
	_ = d.ln.Close()
	d.dropConnections()
}
