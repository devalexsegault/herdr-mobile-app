package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBoardCaller stands in for boardd: it answers board.get with fixed
// columns, hands out ids for created columns and records every write.
type fakeBoardCaller struct {
	calls  []string
	nextID int64
}

func (f *fakeBoardCaller) Call(_ context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	switch method {
	case "board.get":
		return json.RawMessage(`{"board":{"id":3,"name":"main"},"columns":[
			{"id":10,"board_id":3,"name":"Todo","position":0,"trigger":"manual"},
			{"id":11,"board_id":3,"name":"Execute","position":1,"trigger":"auto","system_prompt":"old","on_success_column_id":12},
			{"id":12,"board_id":3,"name":"Done","position":2,"trigger":"manual"}]}`), nil
	case "column.create":
		f.nextID++
		f.calls = append(f.calls, method)
		return json.RawMessage(`{"id":` + strings.TrimSpace(json.Number(itoa(f.nextID)).String()) + `}`), nil
	default:
		f.calls = append(f.calls, method)
		return json.RawMessage(`{}`), nil
	}
}

func itoa(value int64) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func testBoardTemplates(t *testing.T) (*boardTemplates, *recordingSender, *fakeBoardCaller, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "board-templates")
	sender := newRecordingSender()
	board := &fakeBoardCaller{nextID: 100}
	templates := newBoardTemplates(dir, board, sender, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return templates, sender, board, dir
}

func lastResult(t *testing.T, sender *recordingSender) map[string]any {
	t.Helper()
	messages := sender.messages("phone")
	if len(messages) == 0 {
		t.Fatal("no message was sent")
	}
	result := messages[len(messages)-1]
	if result["type"] != "board_template_result" {
		t.Fatalf("last message = %v", result)
	}
	return result
}

func TestBoardTemplatesSaveListGetDelete(t *testing.T) {
	templates, sender, _, dir := testBoardTemplates(t)
	ctx := context.Background()

	templates.handle(ctx, "phone", templateRequest{Type: "board_template_list", RequestID: "l1"})
	result := lastResult(t, sender)
	if result["ok"] != true || result["dir"] != dir || len(result["templates"].([]any)) != 0 {
		t.Fatalf("empty list = %v", result)
	}

	body := json.RawMessage(`{"name":"Docs","description":"docs","columns":[{"name":"Write","trigger":"auto","on_success":"Done"},{"name":"Done"}]}`)
	templates.handle(ctx, "phone", templateRequest{Type: "board_template_save", RequestID: "s1", Template: body})
	result = lastResult(t, sender)
	if result["ok"] != true || result["action"] != "save" {
		t.Fatalf("save = %v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "Docs.json")); err != nil {
		t.Fatalf("template file: %v", err)
	}

	templates.handle(ctx, "phone", templateRequest{Type: "board_template_save", RequestID: "s2", Template: json.RawMessage(`{"name":"Bad","columns":[{"name":"A","on_fail":"Missing"}]}`)})
	result = lastResult(t, sender)
	if result["ok"] != false || !strings.Contains(result["error"].(string), "Missing") {
		t.Fatalf("invalid template accepted: %v", result)
	}

	templates.handle(ctx, "phone", templateRequest{Type: "board_template_get", RequestID: "g1", Name: "Docs"})
	result = lastResult(t, sender)
	template := result["template"].(map[string]any)
	if result["ok"] != true || template["name"] != "Docs" || len(template["columns"].([]any)) != 2 {
		t.Fatalf("get = %v", result)
	}

	templates.handle(ctx, "phone", templateRequest{Type: "board_template_delete", RequestID: "d1", Name: "Docs"})
	if result = lastResult(t, sender); result["ok"] != true {
		t.Fatalf("delete = %v", result)
	}
	templates.handle(ctx, "phone", templateRequest{Type: "board_template_get", RequestID: "g2", Name: "Docs"})
	if result = lastResult(t, sender); result["ok"] != false {
		t.Fatalf("deleted template still readable: %v", result)
	}
}

func TestBoardTemplatesExportAndApply(t *testing.T) {
	templates, sender, board, _ := testBoardTemplates(t)
	ctx := context.Background()

	templates.handle(ctx, "phone", templateRequest{Type: "board_template_export", RequestID: "e1", BoardID: 3, Save: true})
	result := lastResult(t, sender)
	if result["ok"] != true || result["board_name"] != "main" || result["saved"] != true {
		t.Fatalf("export = %v", result)
	}
	exported := result["template"].(map[string]any)
	columns := exported["columns"].([]any)
	if exported["name"] != "main" || len(columns) != 3 || columns[1].(map[string]any)["on_success"] != "Done" {
		t.Fatalf("exported template = %v", exported)
	}

	templates.handle(ctx, "phone", templateRequest{Type: "board_template_apply", RequestID: "a1", BoardID: 3, Name: "main", Mode: "replace"})
	result = lastResult(t, sender)
	if result["ok"] != true || result["mode"] != "replace" {
		t.Fatalf("apply = %v", result)
	}
	applied := result["result"].(map[string]any)
	if len(applied["updated"].([]any)) != 3 || len(applied["created"].([]any)) != 0 || len(applied["deleted"].([]any)) != 0 {
		t.Fatalf("applying a board's own export changes nothing but rewrites: %v", applied)
	}
	if len(board.calls) == 0 {
		t.Fatal("apply did not reach the board")
	}

	templates.handle(ctx, "phone", templateRequest{Type: "board_template_apply", RequestID: "a2", BoardID: 3, Name: "Nope", Mode: "append"})
	if result = lastResult(t, sender); result["ok"] != false || !strings.Contains(result["error"].(string), "not found") {
		t.Fatalf("missing template applied: %v", result)
	}
}

func TestBoardTemplatesBriefs(t *testing.T) {
	templates, sender, _, dir := testBoardTemplates(t)
	ctx := context.Background()

	templates.handle(ctx, "phone", templateRequest{Type: "board_template_brief", RequestID: "b1", Kind: "design", Name: "Docs sprint", Intent: "docs"})
	result := lastResult(t, sender)
	if result["ok"] != true || result["cwd"] != dir || !strings.Contains(result["prompt"].(string), "Docs sprint.json") {
		t.Fatalf("design brief = %v", result)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("the templates directory must exist for the agent to start in it: %v", err)
	}

	templates.handle(ctx, "phone", templateRequest{Type: "board_template_brief", RequestID: "b2", Kind: "edit", BoardID: 3, Intent: "add QA"})
	result = lastResult(t, sender)
	prompt, _ := result["prompt"].(string)
	if result["ok"] != true || result["name"] != "main" || !strings.Contains(prompt, "--board 3") || !strings.Contains(prompt, `"name": "Execute"`) {
		t.Fatalf("edit brief = %v", result)
	}

	templates.handle(ctx, "phone", templateRequest{Type: "board_template_brief", RequestID: "b3", Kind: "design", Name: "../x"})
	if result = lastResult(t, sender); result["ok"] != false {
		t.Fatalf("unsafe name accepted: %v", result)
	}
}
