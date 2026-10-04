package slashcmd

import (
	"os"
	"path/filepath"
	"testing"
)

func writePluginFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, content)
}

func TestClaudeListsEnabledPluginSkillsAndCommands(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(t.TempDir(), "app")
	os.MkdirAll(filepath.Join(project, ".git"), 0o755)
	cache := filepath.Join(home, ".claude", "plugins", "cache")
	userPlugin := filepath.Join(cache, "market", "toolkit", "1.0.0")
	projectPlugin := filepath.Join(cache, "market", "builder", "2.0.0")
	otherProjectPlugin := filepath.Join(cache, "market", "elsewhere", "1.0.0")
	disabledPlugin := filepath.Join(cache, "market", "muted", "1.0.0")

	// Skills listed by the manifest, plus a commands tree.
	writePluginFile(t, filepath.Join(userPlugin, ".claude-plugin", "plugin.json"), `{"name":"toolkit","skills":["./skills/onboard"]}`)
	writePluginFile(t, filepath.Join(userPlugin, "skills", "onboard", "SKILL.md"), "---\nname: 00-onboard\ndescription: Start here\n---\n")
	writePluginFile(t, filepath.Join(userPlugin, "skills", "unlisted", "SKILL.md"), "---\nname: unlisted\n---\n")
	writePluginFile(t, filepath.Join(userPlugin, "commands", "ship.md"), "---\ndescription: Ship it\n---\n")
	// No manifest skills: the default skills/ directory.
	writePluginFile(t, filepath.Join(projectPlugin, "skills", "plan", "SKILL.md"), "---\nname: plan\ndescription: Plan work\n---\n")
	writePluginFile(t, filepath.Join(otherProjectPlugin, "skills", "x", "SKILL.md"), "---\nname: x\n---\n")
	writePluginFile(t, filepath.Join(disabledPlugin, "skills", "y", "SKILL.md"), "---\nname: y\n---\n")

	writePluginFile(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), `{"version":2,"plugins":{
		"toolkit@market":[{"scope":"user","installPath":"`+userPlugin+`"}],
		"builder@market":[{"scope":"project","projectPath":"`+project+`","installPath":"`+projectPlugin+`"}],
		"elsewhere@market":[{"scope":"project","projectPath":"/somewhere/else","installPath":"`+otherProjectPlugin+`"}],
		"muted@market":[{"scope":"user","installPath":"`+disabledPlugin+`"}]
	}}`)
	writePluginFile(t, filepath.Join(home, ".claude", "settings.json"), `{"enabledPlugins":{"toolkit@market":true,"muted@market":true,"elsewhere@market":true}}`)
	writePluginFile(t, filepath.Join(project, ".claude", "settings.json"), `{"enabledPlugins":{"builder@market":true,"muted@market":false}}`)

	catalog := CatalogFor("claude", project, home)
	for _, name := range []string{"/toolkit:00-onboard", "/toolkit:ship", "/builder:plan"} {
		if !hasCommand(catalog, name) {
			t.Errorf("missing %s", name)
		}
	}
	for _, name := range []string{"/toolkit:unlisted", "/elsewhere:x", "/muted:y"} {
		if hasCommand(catalog, name) {
			t.Errorf("unexpected %s", name)
		}
	}
	for _, command := range catalog.Commands {
		if command.Command == "/toolkit:00-onboard" && (command.Description != "Start here" || command.Source != "plugin") {
			t.Errorf("onboard = %+v", command)
		}
	}
}
