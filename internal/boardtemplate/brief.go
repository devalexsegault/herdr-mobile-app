package boardtemplate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// The rules an agent needs to design a pipeline, distilled from the board's
// own skill document so a template written on the phone follows the same
// conventions as one written by hand.
const boardRules = `Board rules (herdr-board):
- A board is a list of columns; each card is ONE deliverable that a single agent run can finish.
- A column with trigger "auto" starts an agent on every card that enters it; "manual" columns are hand-managed stages (Backlog, Ready, Blocked, Human Review, Done).
- An auto column's system_prompt is the whole brief its agent gets, on top of the card's own description. Write it in the second person, say what the stage must produce, how it reports (board comment then board done --outcome ok|fail), and what it must never do.
- on_success and on_fail name the columns a card moves to when the run reports ok or fail. Every auto column should define both; manual columns usually define neither.
- Overrides: harness (claude, codex, ...), model, effort (low|medium|high), permission (for Claude Code: default, acceptEdits, plan, bypassPermissions), timeout_minutes, fresh_session (true starts each run in a new session).
- Choose model and effort per stage: opus with high effort where judgement is expensive (design, review, arbitration), sonnet for prescribed execution, haiku for mechanical steps. Do not give every stage the same setting without a reason.
- Keep column names short and distinct; they are what people tap on a phone.`

const templateFormat = `Template file format (JSON, one file per template):
{
  "name": "<same as the file name without .json>",
  "description": "<one line>",
  "columns": [
    {
      "name": "Backlog", "trigger": "manual"
    },
    {
      "name": "Execute", "trigger": "auto", "system_prompt": "...", "harness": "claude", "model": "sonnet", "effort": "medium",
      "permission": "acceptEdits", "timeout_minutes": 60, "fresh_session": true, "on_success": "Review", "on_fail": "Blocked"
    }
  ]
}
Only these fields are accepted; omit what does not apply. on_success/on_fail must name columns of the same template.`

// DesignBrief is the first prompt of an agent asked to create a template
// from a one-line intent. It runs inside the templates directory.
func DesignBrief(dir, name, intent string) string {
	file := filepath.Join(dir, name+".json")
	var b strings.Builder
	fmt.Fprintf(&b, "You are designing a herdr-board pipeline template named %q.\n", name)
	if strings.TrimSpace(intent) != "" {
		fmt.Fprintf(&b, "What it is for: %s\n", strings.TrimSpace(intent))
	}
	fmt.Fprintf(&b, "\nWrite it to %s. Your working directory is the templates directory; other *.json files there are existing templates you may read for inspiration, but do not modify them.\n\n", file)
	b.WriteString(templateFormat)
	b.WriteString("\n\n")
	b.WriteString(boardRules)
	b.WriteString("\n\nWork like this: ask me the questions you need (stages, who reviews, how strict), propose the pipeline in a few lines, then write the file. After writing it, print the column list with one line per column and stop. Do not create boards or cards.")
	return b.String()
}

// EditBrief is the first prompt of an agent asked to change an existing
// board's columns. It receives the board as a template and applies the
// changes itself with the board CLI.
func EditBrief(dir string, boardID int64, boardName string, current Template, intent string) string {
	encoded, _ := json.MarshalIndent(current, "", "  ")
	var b strings.Builder
	fmt.Fprintf(&b, "You are editing the columns of the herdr-board board %q (board id %d).\n", boardName, boardID)
	if strings.TrimSpace(intent) != "" {
		fmt.Fprintf(&b, "What I want changed: %s\n", strings.TrimSpace(intent))
	}
	b.WriteString("\nThe board's current columns, as a template:\n")
	b.Write(encoded)
	b.WriteString("\n\n")
	b.WriteString(boardRules)
	b.WriteString("\n\nApply the changes yourself with the board CLI, always with --board ")
	fmt.Fprintf(&b, "%d:\n", boardID)
	fmt.Fprintf(&b, "  board column list --board %d\n", boardID)
	fmt.Fprintf(&b, "  board column create --board %d --name NAME --trigger auto|manual --prompt \"...\" [--harness H --model M --effort E --permission P --timeout MIN --fresh-session|--reuse-session] [--position N]\n", boardID)
	fmt.Fprintf(&b, "  board column edit COLUMN --board %d [--name ...] [--prompt \"...\"] [--trigger ...] [--on-success COLUMN|--clear-on-success] [--on-fail COLUMN|--clear-on-fail] [--harness ...|--clear-harness] [--model ...|--clear-model] [--effort ...|--clear-effort] [--permission ...|--clear-permission] [--timeout MIN]\n", boardID)
	fmt.Fprintf(&b, "  board column reorder COLUMN --board %d --position N\n", boardID)
	fmt.Fprintf(&b, "  board column delete COLUMN --board %d\n", boardID)
	b.WriteString("Run `board column --help` and `board skill` if you need details. Cards keep their column; do not create, move or delete cards, and never delete a column that still holds cards without asking me first. Confirm the plan with me before the first write, then report exactly what you changed.\n")
	fmt.Fprintf(&b, "If I also ask you to keep the result as a reusable template, write it to %s.\n", filepath.Join(dir, "<name>.json"))
	return b.String()
}
