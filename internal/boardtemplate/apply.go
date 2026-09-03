package boardtemplate

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Caller is the slice of the boardd client the planner needs.
type Caller interface {
	Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
}

// SnapshotColumn is boardd's column as `board.get` returns it.
type SnapshotColumn struct {
	ID                 int64   `json:"id"`
	BoardID            int64   `json:"board_id"`
	Name               string  `json:"name"`
	Position           int64   `json:"position"`
	Trigger            string  `json:"trigger"`
	FreshSession       bool    `json:"fresh_session"`
	HarnessOverride    *string `json:"harness_override"`
	ModelOverride      *string `json:"model_override"`
	EffortOverride     *string `json:"effort_override"`
	PermissionOverride *string `json:"permission_override"`
	SystemPrompt       *string `json:"system_prompt"`
	OnSuccessColumnID  *int64  `json:"on_success_column_id"`
	OnFailColumnID     *int64  `json:"on_fail_column_id"`
	TimeoutMinutes     *int64  `json:"timeout_minutes"`
}

type snapshot struct {
	Board struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"board"`
	Columns []SnapshotColumn `json:"columns"`
}

// Snapshot reads a board's name and columns, the columns sorted by position.
func Snapshot(ctx context.Context, caller Caller, boardID int64) (string, []SnapshotColumn, error) {
	params, _ := json.Marshal(map[string]any{"board_id": boardID})
	raw, err := caller.Call(ctx, "board.get", params)
	if err != nil {
		return "", nil, err
	}
	var decoded snapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", nil, fmt.Errorf("board snapshot is not readable: %w", err)
	}
	sort.SliceStable(decoded.Columns, func(i, j int) bool {
		return decoded.Columns[i].Position < decoded.Columns[j].Position
	})
	return decoded.Board.Name, decoded.Columns, nil
}

// FromSnapshot turns a board's columns into a template, resolving transition
// ids back to names so the result is portable.
func FromSnapshot(name, description string, columns []SnapshotColumn) Template {
	names := make(map[int64]string, len(columns))
	for _, column := range columns {
		names[column.ID] = column.Name
	}
	template := Template{Name: name, Description: description, Columns: make([]Column, 0, len(columns))}
	for _, column := range columns {
		entry := Column{
			Name:           column.Name,
			Trigger:        column.Trigger,
			SystemPrompt:   deref(column.SystemPrompt),
			Harness:        deref(column.HarnessOverride),
			Model:          deref(column.ModelOverride),
			Effort:         deref(column.EffortOverride),
			Permission:     deref(column.PermissionOverride),
			TimeoutMinutes: column.TimeoutMinutes,
			FreshSession:   column.FreshSession,
		}
		if column.OnSuccessColumnID != nil {
			entry.OnSuccess = names[*column.OnSuccessColumnID]
		}
		if column.OnFailColumnID != nil {
			entry.OnFail = names[*column.OnFailColumnID]
		}
		template.Columns = append(template.Columns, entry)
	}
	return template.Normalized()
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// Result says what Apply did, by column name, so the phone can confirm it.
type Result struct {
	Created []string `json:"created"`
	Updated []string `json:"updated"`
	Deleted []string `json:"deleted"`
}

// Apply replays a template onto a board the way a person would with the CLI:
// columns are matched by name, missing ones created, and transitions set in a
// second pass once every column has an id. Mode "replace" also updates the
// matched columns, deletes the board's other columns (their cards move to the
// template's first column) and reorders to the template's order; "append"
// only adds what is missing and leaves existing columns alone.
func Apply(ctx context.Context, caller Caller, boardID int64, template Template, mode string) (Result, error) {
	template = template.Normalized()
	if err := template.Validate(); err != nil {
		return Result{}, err
	}
	switch mode {
	case "replace", "append":
	default:
		return Result{}, fmt.Errorf("mode %q is not replace or append", mode)
	}
	_, existing, err := Snapshot(ctx, caller, boardID)
	if err != nil {
		return Result{}, err
	}
	byName := make(map[string]SnapshotColumn, len(existing))
	for _, column := range existing {
		byName[strings.ToLower(column.Name)] = column
	}

	result := Result{Created: []string{}, Updated: []string{}, Deleted: []string{}}
	ids := make(map[string]int64, len(template.Columns))
	touched := make(map[string]bool, len(template.Columns))
	for index, column := range template.Columns {
		key := strings.ToLower(column.Name)
		if current, ok := byName[key]; ok {
			ids[key] = current.ID
			if mode == "replace" {
				if err := call(ctx, caller, "column.update", updateParams(current.ID, column)); err != nil {
					return result, fmt.Errorf("update column %q: %w", column.Name, err)
				}
				result.Updated = append(result.Updated, column.Name)
				touched[key] = true
			}
			continue
		}
		params := createParams(boardID, column)
		if mode == "replace" {
			params["position"] = index
		}
		raw, err := caller.Call(ctx, "column.create", mustJSON(params))
		if err != nil {
			return result, fmt.Errorf("create column %q: %w", column.Name, err)
		}
		var created struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &created); err != nil || created.ID == 0 {
			return result, fmt.Errorf("create column %q: the board did not return its id", column.Name)
		}
		ids[key] = created.ID
		result.Created = append(result.Created, column.Name)
		touched[key] = true
	}

	if mode == "replace" {
		first := ids[strings.ToLower(template.Columns[0].Name)]
		for _, column := range existing {
			if _, keep := ids[strings.ToLower(column.Name)]; keep {
				continue
			}
			params := map[string]any{"id": column.ID, "move_cards_to": first}
			if err := call(ctx, caller, "column.delete", params); err != nil {
				return result, fmt.Errorf("delete column %q: %w", column.Name, err)
			}
			result.Deleted = append(result.Deleted, column.Name)
		}
		for index, column := range template.Columns {
			params := map[string]any{"id": ids[strings.ToLower(column.Name)], "position": index}
			if err := call(ctx, caller, "column.reorder", params); err != nil {
				return result, fmt.Errorf("reorder column %q: %w", column.Name, err)
			}
		}
	}

	// Transitions last: every target now has an id. Append leaves the
	// existing columns' own transitions alone.
	for _, column := range template.Columns {
		key := strings.ToLower(column.Name)
		if !touched[key] {
			continue
		}
		params := map[string]any{
			"id":                   ids[key],
			"on_success_column_id": transitionID(ids, column.OnSuccess),
			"on_fail_column_id":    transitionID(ids, column.OnFail),
		}
		if err := call(ctx, caller, "column.update", params); err != nil {
			return result, fmt.Errorf("set transitions of %q: %w", column.Name, err)
		}
	}
	return result, nil
}

func transitionID(ids map[string]int64, name string) any {
	if name == "" {
		return nil
	}
	if id, ok := ids[strings.ToLower(name)]; ok {
		return id
	}
	return nil
}

func createParams(boardID int64, column Column) map[string]any {
	params := map[string]any{
		"board_id":      boardID,
		"name":          column.Name,
		"trigger":       column.Trigger,
		"fresh_session": column.FreshSession,
	}
	setIfPresent(params, "system_prompt", column.SystemPrompt)
	setIfPresent(params, "harness_override", column.Harness)
	setIfPresent(params, "model_override", column.Model)
	setIfPresent(params, "effort_override", column.Effort)
	setIfPresent(params, "permission_override", column.Permission)
	if column.TimeoutMinutes != nil {
		params["timeout_minutes"] = *column.TimeoutMinutes
	}
	return params
}

// updateParams sends every override explicitly: boardd's update treats an
// omitted field as "keep", and a template's empty override means "none".
func updateParams(id int64, column Column) map[string]any {
	params := map[string]any{
		"id":                  id,
		"name":                column.Name,
		"trigger":             column.Trigger,
		"fresh_session":       column.FreshSession,
		"system_prompt":       nullable(column.SystemPrompt),
		"harness_override":    nullable(column.Harness),
		"model_override":      nullable(column.Model),
		"effort_override":     nullable(column.Effort),
		"permission_override": nullable(column.Permission),
		"timeout_minutes":     nil,
	}
	if column.TimeoutMinutes != nil {
		params["timeout_minutes"] = *column.TimeoutMinutes
	}
	return params
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func setIfPresent(params map[string]any, key, value string) {
	if value != "" {
		params[key] = value
	}
}

func call(ctx context.Context, caller Caller, method string, params map[string]any) error {
	_, err := caller.Call(ctx, method, mustJSON(params))
	return err
}

func mustJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}
