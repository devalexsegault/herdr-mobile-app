package herdr

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Runs against the Herdr binary on this machine and skips everywhere else.
// Read-only: it only lists sessions.
func TestLiveSessionsRefresh(t *testing.T) {
	bin := filepath.Join(os.Getenv("HOME"), ".local", "bin", "herdr")
	socket := filepath.Join(os.Getenv("HOME"), ".config", "herdr", "herdr.sock")
	if _, err := os.Stat(bin); err != nil {
		t.Skip("no herdr binary")
	}
	if _, err := os.Stat(socket); err != nil {
		t.Skip("no default herdr socket")
	}
	sessions := NewSessions(bin, NewClient(bin, socket))
	infos, err := sessions.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	for _, info := range infos {
		t.Logf("session %q default=%v prefix=%q socket=%s", info.Name, info.Default, sessions.Prefix(info.Name), info.SocketPath)
	}
	if len(infos) == 0 {
		t.Fatal("no running session listed")
	}
}
