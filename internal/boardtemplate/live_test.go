package boardtemplate

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/board"
)

// Exporting a real board through a live daemon, skipped when none answers.
// Read-only on purpose: a developer's daemon owns real runs, so this only
// proves that boardd's snapshot shape still turns into a valid template.
func TestExportAgainstLiveDaemon(t *testing.T) {
	socketPath := board.DefaultSocketPath(os.Getenv("XDG_DATA_HOME"))
	if _, err := os.Stat(socketPath); err != nil {
		t.Skipf("no boardd socket at %s", socketPath)
	}
	client := board.New(socketPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(client.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, ok := client.Status(ctx); !ok {
		t.Skipf("boardd socket at %s is not answering", socketPath)
	}
	raw, err := client.Call(ctx, "board.list", json.RawMessage(`{"all":true}`))
	if err != nil {
		t.Skipf("board.list: %v", err)
	}
	var listing struct {
		Boards []struct {
			ID int64 `json:"id"`
		} `json:"boards"`
	}
	if json.Unmarshal(raw, &listing) != nil || len(listing.Boards) == 0 {
		t.Skip("no board to export")
	}
	name, columns, err := Snapshot(ctx, client, listing.Boards[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	template := FromSnapshot("Live export", "", columns)
	if err := template.Validate(); err != nil {
		t.Fatalf("board %q (%d) does not export to a valid template: %v", name, listing.Boards[0].ID, err)
	}
	if len(template.Columns) != len(columns) {
		t.Fatalf("exported %d columns of %d", len(template.Columns), len(columns))
	}
}
