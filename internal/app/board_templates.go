package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/0cv/herdr-mobile-relay/internal/boardtemplate"
)

// boardTemplates answers the phone's template messages. Templates are the
// relay's own files; the board daemon is only consulted to export a board's
// columns or to replay a template onto one.
type boardTemplates struct {
	store  *boardtemplate.Store
	board  boardtemplate.Caller
	sender boardSender
	logger *slog.Logger
}

func newBoardTemplates(dir string, board boardtemplate.Caller, sender boardSender, logger *slog.Logger) *boardTemplates {
	return &boardTemplates{store: boardtemplate.NewStore(dir), board: board, sender: sender, logger: logger}
}

// templateRequest is the slice of an inbound message the handlers read.
type templateRequest struct {
	Type        string
	RequestID   string
	Name        string
	Description string
	Template    json.RawMessage
	BoardID     int64
	Mode        string
	Kind        string
	Intent      string
	Save        bool
}

func (t *boardTemplates) handle(ctx context.Context, clientID string, request templateRequest) {
	if t == nil {
		return
	}
	action := strings.TrimPrefix(request.Type, "board_template_")
	payload, err := t.dispatch(ctx, action, request)
	message := map[string]any{
		"type":       "board_template_result",
		"request_id": request.RequestID,
		"action":     action,
		"ok":         err == nil,
	}
	if err != nil {
		message["error"] = err.Error()
		if !errors.Is(err, boardtemplate.ErrNotFound) {
			t.logger.Warn("board template request failed", "action", action, "error", err)
		}
	}
	for key, value := range payload {
		message[key] = value
	}
	t.sender.SendByID(clientID, message)
}

func (t *boardTemplates) dispatch(ctx context.Context, action string, request templateRequest) (map[string]any, error) {
	switch action {
	case "list":
		templates, err := t.store.List()
		if err != nil {
			return nil, err
		}
		return map[string]any{"templates": templates, "dir": t.store.Dir()}, nil
	case "get":
		template, err := t.store.Get(request.Name)
		if err != nil {
			return nil, err
		}
		return map[string]any{"template": template}, nil
	case "save":
		if len(request.Template) == 0 {
			return nil, errors.New("a template is required")
		}
		template, err := boardtemplate.Decode(request.Template)
		if err != nil {
			return nil, err
		}
		saved, err := t.store.Save(template)
		if err != nil {
			return nil, err
		}
		return map[string]any{"template": saved}, nil
	case "delete":
		if err := t.store.Delete(request.Name); err != nil {
			return nil, err
		}
		return map[string]any{"name": request.Name}, nil
	case "export":
		if t.board == nil {
			return nil, errors.New("the board daemon is unavailable")
		}
		if request.BoardID <= 0 {
			return nil, errors.New("a board id is required")
		}
		boardName, columns, err := boardtemplate.Snapshot(ctx, t.board, request.BoardID)
		if err != nil {
			return nil, fmt.Errorf("read board: %w", err)
		}
		name := strings.TrimSpace(request.Name)
		if name == "" {
			name = boardName
		}
		template := boardtemplate.FromSnapshot(name, request.Description, columns)
		if err := template.Validate(); err != nil {
			return nil, err
		}
		if request.Save {
			if _, err := t.store.Save(template); err != nil {
				return nil, err
			}
		}
		return map[string]any{"template": template, "board_name": boardName, "saved": request.Save}, nil
	case "apply":
		if t.board == nil {
			return nil, errors.New("the board daemon is unavailable")
		}
		if request.BoardID <= 0 {
			return nil, errors.New("a board id is required")
		}
		template, err := t.store.Get(request.Name)
		if err != nil {
			return nil, err
		}
		mode := request.Mode
		if mode == "" {
			mode = "append"
		}
		result, err := boardtemplate.Apply(ctx, t.board, request.BoardID, template, mode)
		if err != nil {
			return map[string]any{"result": result}, err
		}
		return map[string]any{"result": result, "mode": mode}, nil
	case "brief":
		return t.brief(ctx, request)
	default:
		return nil, fmt.Errorf("unknown template action %q", action)
	}
}

// brief writes nothing but the templates directory and returns what the app
// needs to start a designing agent: where to run it and its first prompt.
func (t *boardTemplates) brief(ctx context.Context, request templateRequest) (map[string]any, error) {
	if err := t.store.EnsureDir(); err != nil {
		return nil, err
	}
	switch request.Kind {
	case "design":
		if err := boardtemplate.ValidateName(strings.TrimSpace(request.Name)); err != nil {
			return nil, err
		}
		name := strings.TrimSpace(request.Name)
		return map[string]any{
			"kind":   "design",
			"name":   name,
			"cwd":    t.store.Dir(),
			"prompt": boardtemplate.DesignBrief(t.store.Dir(), name, request.Intent),
			"label":  "Template: " + name,
		}, nil
	case "edit":
		if t.board == nil {
			return nil, errors.New("the board daemon is unavailable")
		}
		if request.BoardID <= 0 {
			return nil, errors.New("a board id is required")
		}
		boardName, columns, err := boardtemplate.Snapshot(ctx, t.board, request.BoardID)
		if err != nil {
			return nil, fmt.Errorf("read board: %w", err)
		}
		current := boardtemplate.FromSnapshot(boardName, "", columns)
		return map[string]any{
			"kind":   "edit",
			"name":   boardName,
			"cwd":    t.store.Dir(),
			"prompt": boardtemplate.EditBrief(t.store.Dir(), request.BoardID, boardName, current, request.Intent),
			"label":  "Board: " + boardName,
		}, nil
	default:
		return nil, fmt.Errorf("brief kind %q is not design or edit", request.Kind)
	}
}
