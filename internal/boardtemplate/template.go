// Package boardtemplate stores reusable herdr-board pipelines as plain JSON
// files and replays them onto boards through boardd's column methods.
//
// boardd knows one built-in template and no user-defined ones, so the relay
// owns this: a template is a list of columns with their prompts, triggers,
// overrides and transitions, independent of any project. The files are meant
// to be read and written by people and by coding agents alike, which is why
// transitions reference columns by name rather than by database id.
package boardtemplate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Column is one pipeline stage. Empty strings mean "no override"; a template
// never carries database ids, so OnSuccess and OnFail name columns of the same
// template.
type Column struct {
	Name           string `json:"name"`
	Trigger        string `json:"trigger,omitempty"`
	SystemPrompt   string `json:"system_prompt,omitempty"`
	Harness        string `json:"harness,omitempty"`
	Model          string `json:"model,omitempty"`
	Effort         string `json:"effort,omitempty"`
	Permission     string `json:"permission,omitempty"`
	TimeoutMinutes *int64 `json:"timeout_minutes,omitempty"`
	FreshSession   bool   `json:"fresh_session,omitempty"`
	OnSuccess      string `json:"on_success,omitempty"`
	OnFail         string `json:"on_fail,omitempty"`
}

// Template is a named pipeline. The name doubles as the file name.
type Template struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Columns     []Column `json:"columns"`
}

const (
	maxNameLength   = 64
	maxColumns      = 40
	maxPromptLength = 64 * 1024
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]*$`)

// ValidateName accepts what is safe as a file name on every platform and
// readable as a title on a phone.
func ValidateName(name string) error {
	if name == "" {
		return errors.New("a template name is required")
	}
	if len(name) > maxNameLength {
		return fmt.Errorf("a template name is at most %d characters", maxNameLength)
	}
	if !namePattern.MatchString(name) || strings.HasSuffix(name, " ") || strings.HasSuffix(name, ".") {
		return errors.New("a template name uses letters, digits, spaces, dots, dashes and underscores")
	}
	return nil
}

// Validate checks the template as a whole: a valid name, distinct column
// names, known triggers, and transitions that point at columns it contains.
func (t Template) Validate() error {
	if err := ValidateName(t.Name); err != nil {
		return err
	}
	if len(t.Columns) == 0 {
		return errors.New("a template needs at least one column")
	}
	if len(t.Columns) > maxColumns {
		return fmt.Errorf("a template has at most %d columns", maxColumns)
	}
	names := make(map[string]bool, len(t.Columns))
	for index, column := range t.Columns {
		name := strings.TrimSpace(column.Name)
		if name == "" {
			return fmt.Errorf("column %d has no name", index+1)
		}
		if names[strings.ToLower(name)] {
			return fmt.Errorf("column name %q is used twice", name)
		}
		names[strings.ToLower(name)] = true
		switch column.Trigger {
		case "", "manual", "auto":
		default:
			return fmt.Errorf("column %q has trigger %q; use manual or auto", name, column.Trigger)
		}
		if len(column.SystemPrompt) > maxPromptLength {
			return fmt.Errorf("column %q has a prompt longer than %d bytes", name, maxPromptLength)
		}
		if column.TimeoutMinutes != nil && *column.TimeoutMinutes <= 0 {
			return fmt.Errorf("column %q has a timeout that is not positive", name)
		}
	}
	for _, column := range t.Columns {
		for _, target := range []string{column.OnSuccess, column.OnFail} {
			if target != "" && !names[strings.ToLower(strings.TrimSpace(target))] {
				return fmt.Errorf("column %q points at %q, which the template does not contain", column.Name, target)
			}
		}
	}
	return nil
}

// Normalized returns the template with names trimmed and triggers defaulted,
// which is what gets stored and applied.
func (t Template) Normalized() Template {
	out := t
	out.Name = strings.TrimSpace(t.Name)
	out.Description = strings.TrimSpace(t.Description)
	out.Columns = make([]Column, len(t.Columns))
	for index, column := range t.Columns {
		column.Name = strings.TrimSpace(column.Name)
		if column.Trigger == "" {
			column.Trigger = "manual"
		}
		column.OnSuccess = strings.TrimSpace(column.OnSuccess)
		column.OnFail = strings.TrimSpace(column.OnFail)
		out.Columns[index] = column
	}
	return out
}

// Decode parses a template, refusing unknown fields so a typo in a
// hand-written file is reported rather than silently dropped.
func Decode(data []byte) (Template, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var template Template
	if err := decoder.Decode(&template); err != nil {
		return Template{}, fmt.Errorf("template is not valid: %w", err)
	}
	template = template.Normalized()
	if err := template.Validate(); err != nil {
		return Template{}, err
	}
	return template, nil
}

// Store keeps one file per template in a directory.
type Store struct {
	dir string
}

// NewStore returns a store rooted at dir; the directory is created on the
// first write.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Dir is where the files live, which is also where an agent designing a
// template is pointed.
func (s *Store) Dir() string {
	return s.dir
}

// EnsureDir creates the directory so an agent can be started inside it.
func (s *Store) EnsureDir() error {
	return os.MkdirAll(s.dir, 0o700)
}

func (s *Store) path(name string) string {
	return filepath.Join(s.dir, name+".json")
}

// List returns every readable template, sorted by name. Unreadable files are
// skipped: one broken hand-written file must not hide the others.
func (s *Store) List() ([]Template, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Template{}, nil
	}
	if err != nil {
		return nil, err
	}
	templates := make([]Template, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		template, err := s.Get(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			continue
		}
		templates = append(templates, template)
	}
	sort.Slice(templates, func(i, j int) bool {
		return strings.ToLower(templates[i].Name) < strings.ToLower(templates[j].Name)
	})
	return templates, nil
}

// ErrNotFound is returned for a template name that has no file.
var ErrNotFound = errors.New("template not found")

// Get reads one template by name.
func (s *Store) Get(name string) (Template, error) {
	if err := ValidateName(name); err != nil {
		return Template{}, err
	}
	data, err := os.ReadFile(s.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return Template{}, ErrNotFound
	}
	if err != nil {
		return Template{}, err
	}
	template, err := Decode(data)
	if err != nil {
		return Template{}, fmt.Errorf("%s: %w", name, err)
	}
	// The file name is the identity; a body that disagrees is corrected on
	// read rather than producing two names for one template.
	template.Name = name
	return template, nil
}

// Save validates and writes a template atomically.
func (s *Store) Save(template Template) (Template, error) {
	template = template.Normalized()
	if err := template.Validate(); err != nil {
		return Template{}, err
	}
	if err := s.EnsureDir(); err != nil {
		return Template{}, err
	}
	data, err := json.MarshalIndent(template, "", "  ")
	if err != nil {
		return Template{}, err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(s.dir, ".template-*.json")
	if err != nil {
		return Template{}, err
	}
	tempPath := temp.Name()
	cleanup := func() {
		temp.Close()
		os.Remove(tempPath)
	}
	if _, err := temp.Write(data); err != nil {
		cleanup()
		return Template{}, err
	}
	if err := temp.Chmod(0o600); err != nil {
		cleanup()
		return Template{}, err
	}
	if err := temp.Close(); err != nil {
		os.Remove(tempPath)
		return Template{}, err
	}
	if err := os.Rename(tempPath, s.path(template.Name)); err != nil {
		os.Remove(tempPath)
		return Template{}, err
	}
	return template, nil
}

// Delete removes a template; a missing one is ErrNotFound.
func (s *Store) Delete(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	err := os.Remove(s.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	return err
}
