package board

import "sort"

// allowedMethods is the exact set of boardd protocol v1 methods the relay will
// proxy on behalf of a paired phone, mapped to whether the call mutates board
// state (which decides whether the relay writes an audit record).
//
// The boardd socket is mode 0600: local filesystem access is its whole
// authentication boundary. Bridging it to a remote phone removes that boundary,
// so the bridge re-establishes one here. Anything absent from this table is
// refused before a byte reaches the socket, which is why the table is an
// allowlist and not a denylist — a method added by a future boardd release is
// refused until this relay is taught about it.
//
// Deliberately absent:
//   - daemon.stop — a phone must not be able to stop the daemon that owns
//     running agent panes.
//   - pane.* — the relay already exposes its own audited pane primitives, with
//     the pane-size leases and watch bookkeeping the phone protocol depends on.
//     A second, unaudited path to the same panes would bypass all of it.
var allowedMethods = map[string]bool{
	// Daemon health. Read-only, and the probe behind the board_v1 capability.
	"daemon.status": false,

	// Projects.
	"project.list":     false,
	"project.get":      false,
	"project.selected": false,
	"project.create":   true,
	"project.open":     true,
	"project.select":   true,

	// Boards.
	"board.list":   false,
	"board.get":    false,
	"board.create": true,
	"board.rename": true,
	"board.open":   true,
	"board.select": true,

	// Columns.
	"column.create":  true,
	"column.update":  true,
	"column.delete":  true,
	"column.reorder": true,

	// Cards. Moving a card into an auto column dispatches a real agent, so
	// card.move and card.create are the two highest-consequence calls here and
	// are audited like any other agent-starting mutation.
	"card.create":    true,
	"card.get":       false,
	"card.list":      false,
	"card.update":    true,
	"card.move":      true,
	"card.archive":   true,
	"card.delete":    true,
	"card.duplicate": true,

	// Comments. comment.history is read-only; the rest edit the prompt material
	// that a later dispatch snapshots.
	"comment.add":     true,
	"comment.get":     false,
	"comment.history": false,
	"comment.update":  true,
	"comment.delete":  true,

	// Runs.
	"run.cancel": true,
	"run.retry":  true,
	"run.done":   true,
	"run.focus":  true,

	// Pickers. Read-only lists the app needs to render harness, session and
	// space selectors without inventing their contents.
	"harness.list":         false,
	"harness.capabilities": false,
	"session.list":         false,
	"space.list":           false,

	// Templates.
	"template.apply": true,
}

// Allowed reports whether the relay proxies method at all.
func Allowed(method string) bool {
	_, ok := allowedMethods[method]
	return ok
}

// Mutating reports whether method changes board state. Unknown methods are
// reported as mutating so a caller that audits before checking Allowed can
// never under-record.
func Mutating(method string) bool {
	mutating, ok := allowedMethods[method]
	return !ok || mutating
}

// AllowedMethods returns the sorted allowlist. The relay sends it to the app so
// a phone can hide controls this relay would refuse instead of discovering the
// refusal by tapping a button.
func AllowedMethods() []string {
	methods := make([]string, 0, len(allowedMethods))
	for method := range allowedMethods {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	return methods
}
