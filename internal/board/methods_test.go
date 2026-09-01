package board

import (
	"slices"
	"strings"
	"testing"
)

func TestAllowlistRefusesDaemonStopAndPaneControl(t *testing.T) {
	for _, method := range []string{"daemon.stop", "pane.set_title", "pane.close", "events.subscribe", "", "card.get "} {
		if Allowed(method) {
			t.Errorf("method %q must not be proxied", method)
		}
	}
}

func TestAllowlistCoversTheBoardWorkingSet(t *testing.T) {
	for _, method := range []string{
		"daemon.status", "board.get", "board.list", "card.create", "card.move",
		"card.update", "comment.add", "comment.history", "run.cancel", "run.retry",
		"harness.list", "session.list", "space.list", "template.apply",
	} {
		if !Allowed(method) {
			t.Errorf("method %q should be proxied", method)
		}
	}
}

// Audit records are written from the mutation flag, so an unknown method has to
// read as mutating: a method this relay has never seen must never be recorded
// as a harmless read.
func TestMutatingClassification(t *testing.T) {
	if Mutating("board.get") || Mutating("daemon.status") || Mutating("harness.list") {
		t.Error("read-only methods were classified as mutating")
	}
	if !Mutating("card.move") || !Mutating("comment.add") || !Mutating("run.retry") {
		t.Error("mutating methods were classified as reads")
	}
	if !Mutating("card.explode") {
		t.Error("an unknown method must be classified as mutating")
	}
}

func TestAllowedMethodsIsSortedAndPaneFree(t *testing.T) {
	methods := AllowedMethods()
	if !slices.IsSorted(methods) {
		t.Fatalf("AllowedMethods is not sorted: %v", methods)
	}
	for _, method := range methods {
		if strings.HasPrefix(method, "pane.") || method == "daemon.stop" {
			t.Fatalf("AllowedMethods advertises %q", method)
		}
	}
}
