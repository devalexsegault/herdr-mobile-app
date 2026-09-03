package boardtemplate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pipeline() Template {
	timeout := int64(45)
	return Template{
		Name:        "Review pipeline",
		Description: "Execute then review",
		Columns: []Column{
			{Name: "Backlog"},
			{Name: "Execute", Trigger: "auto", SystemPrompt: "Do the work.", Harness: "claude", Model: "sonnet", Effort: "medium", Permission: "acceptEdits", TimeoutMinutes: &timeout, FreshSession: true, OnSuccess: "Review", OnFail: "Blocked"},
			{Name: "Review", Trigger: "auto", SystemPrompt: "Judge the work.", Model: "opus", Effort: "high", OnSuccess: "Done", OnFail: "Execute"},
			{Name: "Blocked"},
			{Name: "Done"},
		},
	}
}

func TestStoreRoundTripsAndSorts(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "board-templates"))
	if list, err := store.List(); err != nil || len(list) != 0 {
		t.Fatalf("empty store listed %v, %v", list, err)
	}
	saved, err := store.Save(pipeline())
	if err != nil {
		t.Fatal(err)
	}
	if saved.Columns[0].Trigger != "manual" {
		t.Fatalf("a column without a trigger is manual, got %q", saved.Columns[0].Trigger)
	}
	if _, err := store.Save(Template{Name: "Alpha", Columns: []Column{{Name: "Todo"}}}); err != nil {
		t.Fatal(err)
	}
	list, err := store.List()
	if err != nil || len(list) != 2 || list[0].Name != "Alpha" || list[1].Name != "Review pipeline" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	got, err := store.Get("Review pipeline")
	if err != nil || len(got.Columns) != 5 || got.Columns[1].OnSuccess != "Review" || *got.Columns[1].TimeoutMinutes != 45 {
		t.Fatalf("get = %+v, %v", got, err)
	}
	info, err := os.Stat(filepath.Join(store.Dir(), "Review pipeline.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("template file mode = %v, %v", info, err)
	}
	if err := store.Delete("Alpha"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("Alpha"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete = %v, want ErrNotFound", err)
	}
	if _, err := store.Get("Alpha"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete = %v", err)
	}
}

func TestStoreSkipsBrokenFilesAndRejectsBadNames(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.Save(pipeline()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir(), "Broken.json"), []byte(`{"name":"Broken","columns":[{"name":"A","color":"red"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	list, err := store.List()
	if err != nil || len(list) != 1 || list[0].Name != "Review pipeline" {
		t.Fatalf("a broken file must not hide the others: %+v, %v", list, err)
	}
	if _, err := store.Get("Broken"); err == nil || !strings.Contains(err.Error(), "color") {
		t.Fatalf("unknown field must be named: %v", err)
	}
	for _, name := range []string{"", "../escape", "dots.", "trailing ", strings.Repeat("x", 65), "slash/name"} {
		if err := ValidateName(name); err == nil {
			t.Fatalf("name %q was accepted", name)
		}
	}
}

func TestValidateCatchesDanglingTransitionsAndDuplicates(t *testing.T) {
	broken := pipeline()
	broken.Columns[1].OnFail = "Nowhere"
	if err := broken.Validate(); err == nil || !strings.Contains(err.Error(), "Nowhere") {
		t.Fatalf("dangling transition accepted: %v", err)
	}
	duplicate := pipeline()
	duplicate.Columns[4].Name = "backlog"
	if err := duplicate.Validate(); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("duplicate column accepted: %v", err)
	}
	trigger := pipeline()
	trigger.Columns[0].Trigger = "always"
	if err := trigger.Validate(); err == nil {
		t.Fatal("unknown trigger accepted")
	}
}

func TestFromSnapshotResolvesTransitionNames(t *testing.T) {
	prompt := "Judge it"
	model := "opus"
	success, fail := int64(5), int64(2)
	columns := []SnapshotColumn{
		{ID: 2, Name: "Execute", Position: 1, Trigger: "auto"},
		{ID: 1, Name: "Backlog", Position: 0, Trigger: "manual"},
		{ID: 4, Name: "Review", Position: 2, Trigger: "auto", SystemPrompt: &prompt, ModelOverride: &model, OnSuccessColumnID: &success, OnFailColumnID: &fail, FreshSession: true},
		{ID: 5, Name: "Done", Position: 3, Trigger: "manual"},
	}
	template := FromSnapshot("Exported", "from board", columns)
	if len(template.Columns) != 4 || template.Columns[0].Name != "Execute" {
		t.Fatalf("columns keep the given order: %+v", template.Columns)
	}
	review := template.Columns[2]
	if review.OnSuccess != "Done" || review.OnFail != "Execute" || review.Model != "opus" || review.SystemPrompt != "Judge it" || !review.FreshSession {
		t.Fatalf("review column = %+v", review)
	}
	if err := template.Validate(); err != nil {
		t.Fatal(err)
	}
}

// fakeBoard answers board.get with its columns and records every other call,
// assigning ids to created columns like the daemon does.
type fakeBoard struct {
	columns []SnapshotColumn
	calls   []string
	nextID  int64
}

func (f *fakeBoard) Call(_ context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	var decoded map[string]any
	_ = json.Unmarshal(params, &decoded)
	switch method {
	case "board.get":
		return json.Marshal(map[string]any{"board": map[string]any{"id": 7, "name": "main"}, "columns": f.columns})
	case "column.create":
		f.nextID++
		f.calls = append(f.calls, method+" "+string(params))
		return json.Marshal(map[string]any{"id": f.nextID, "name": decoded["name"]})
	default:
		f.calls = append(f.calls, method+" "+string(params))
		return json.RawMessage(`{}`), nil
	}
}

func (f *fakeBoard) count(method string) int {
	total := 0
	for _, call := range f.calls {
		if strings.HasPrefix(call, method+" ") {
			total++
		}
	}
	return total
}

func TestApplyReplaceReconcilesByName(t *testing.T) {
	stale := "old prompt"
	board := &fakeBoard{nextID: 100, columns: []SnapshotColumn{
		{ID: 1, Name: "Backlog", Position: 0, Trigger: "manual"},
		{ID: 2, Name: "Todo", Position: 1, Trigger: "manual"},
		{ID: 3, Name: "Execute", Position: 2, Trigger: "auto", SystemPrompt: &stale},
	}}
	result, err := Apply(context.Background(), board, 7, pipeline(), "replace")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Created, ",") != "Review,Blocked,Done" ||
		strings.Join(result.Updated, ",") != "Backlog,Execute" ||
		strings.Join(result.Deleted, ",") != "Todo" {
		t.Fatalf("result = %+v", result)
	}
	if board.count("column.create") != 3 || board.count("column.delete") != 1 || board.count("column.reorder") != 5 {
		t.Fatalf("calls = %v", board.calls)
	}
	joined := strings.Join(board.calls, "\n")
	// The extra column's cards go to the template's first column.
	if !strings.Contains(joined, `column.delete {"id":2,"move_cards_to":1}`) {
		t.Fatalf("delete did not move cards to the first column:\n%s", joined)
	}
	// Execute keeps its id (3) and points at the created Review (101) and Blocked (102).
	if !strings.Contains(joined, `"id":3,"on_fail_column_id":102,"on_success_column_id":101`) {
		t.Fatalf("transitions were not resolved to ids:\n%s", joined)
	}
	// Updating a matched column clears overrides the template does not set.
	if !strings.Contains(joined, `"harness_override":null`) || !strings.Contains(joined, `"system_prompt":"Do the work."`) {
		t.Fatalf("matched column was not rewritten explicitly:\n%s", joined)
	}
}

func TestApplyAppendOnlyAddsMissingColumns(t *testing.T) {
	board := &fakeBoard{nextID: 200, columns: []SnapshotColumn{
		{ID: 1, Name: "Backlog", Position: 0},
		{ID: 2, Name: "Execute", Position: 1, Trigger: "auto"},
	}}
	result, err := Apply(context.Background(), board, 7, pipeline(), "append")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Created, ",") != "Review,Blocked,Done" || len(result.Updated) != 0 || len(result.Deleted) != 0 {
		t.Fatalf("result = %+v", result)
	}
	if board.count("column.delete") != 0 || board.count("column.reorder") != 0 {
		t.Fatalf("append must not delete or reorder: %v", board.calls)
	}
	joined := strings.Join(board.calls, "\n")
	// Review is new and points at the existing Execute (2) on failure.
	if !strings.Contains(joined, `"id":201,"on_fail_column_id":2,"on_success_column_id":203`) {
		t.Fatalf("new column transitions:\n%s", joined)
	}
	// The existing Execute column is left untouched, transitions included.
	if strings.Contains(joined, `"id":2,`) {
		t.Fatalf("append touched an existing column:\n%s", joined)
	}
}

func TestApplyRejectsUnknownMode(t *testing.T) {
	if _, err := Apply(context.Background(), &fakeBoard{}, 7, pipeline(), "merge"); err == nil {
		t.Fatal("unknown mode accepted")
	}
}

func TestBriefsCarryFormatRulesAndTargets(t *testing.T) {
	design := DesignBrief("/home/me/templates", "Docs sprint", "write and review documentation")
	for _, want := range []string{"/home/me/templates/Docs sprint.json", `"on_success"`, "trigger \"auto\"", "write and review documentation", "Do not create boards or cards"} {
		if !strings.Contains(design, want) {
			t.Fatalf("design brief lacks %q:\n%s", want, design)
		}
	}
	edit := EditBrief("/home/me/templates", 7, "main", pipeline(), "add a QA stage")
	for _, want := range []string{"board id 7", "board column edit COLUMN --board 7", `"name": "Execute"`, "add a QA stage", "never delete a column that still holds cards"} {
		if !strings.Contains(edit, want) {
			t.Fatalf("edit brief lacks %q:\n%s", want, edit)
		}
	}
}
