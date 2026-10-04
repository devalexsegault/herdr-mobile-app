package slashcmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// claudePluginCommands lists the skills and commands of the Claude Code
// plugins enabled for ctx.Cwd, named "/<plugin>:<name>" as Claude Code
// invokes them. A plugin is enabled by enabledPlugins in the user settings,
// then the project settings from the outermost .claude to the nearest, then
// settings.local.json; the install used is the one recorded for this project,
// else the user-scope one.
func claudePluginCommands(ctx DiscoverContext, budget *int) ([]Command, []string, bool) {
	if ctx.Home == "" {
		return nil, nil, false
	}
	enabled := claudeEnabledPlugins(ctx)
	if len(enabled) == 0 {
		return nil, nil, false
	}
	registryDir := filepath.Join(ctx.Home, ".claude", "plugins")
	data, found, ok := settingsFileIn(registryDir, "installed_plugins.json")
	if !found || !ok {
		return nil, nil, false
	}
	var registry claudeMarketplaceRegistry
	if json.Unmarshal(data, &registry) != nil {
		return nil, nil, false
	}

	keys := make([]string, 0, len(registry.Plugins))
	for key := range registry.Plugins {
		if enabled[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	var commands []Command
	var suppressed []string
	truncated := false
	for _, key := range keys {
		root := claudePluginInstall(ctx, registry.Plugins[key])
		if root == "" {
			continue
		}
		cmds, supp, trunc := claudePluginRootCommands(root, claudePluginName(root, key), budget)
		commands = append(commands, cmds...)
		suppressed = append(suppressed, supp...)
		truncated = truncated || trunc
	}
	return commands, suppressed, truncated
}

func claudeEnabledPlugins(ctx DiscoverContext) map[string]bool {
	paths := []string{filepath.Join(ctx.Home, ".claude", "settings.json")}
	if ctx.Cwd != "" {
		for _, dir := range findProjectDirs(ctx.Cwd, []string{".claude"}) {
			paths = append(paths, filepath.Join(dir, "settings.json"))
		}
		for _, dir := range findProjectDirs(ctx.Cwd, []string{".claude"}) {
			paths = append(paths, filepath.Join(dir, "settings.local.json"))
		}
	}
	enabled := make(map[string]bool)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil || len(data) > maxMetadataSize {
			continue
		}
		var settings struct {
			EnabledPlugins map[string]bool `json:"enabledPlugins"`
		}
		if json.Unmarshal(data, &settings) != nil {
			continue
		}
		for key, value := range settings.EnabledPlugins {
			enabled[key] = value
		}
	}
	return enabled
}

func claudePluginInstall(ctx DiscoverContext, installs []claudePluginInstallRecord) string {
	userRoot := ""
	for _, install := range installs {
		if install.Enabled != nil && !*install.Enabled {
			continue
		}
		root := expandTilde(strings.TrimSpace(install.InstallPath), ctx.Home)
		if root == "" || !filepath.IsAbs(root) {
			continue
		}
		if install.Scope == "project" || install.Scope == "local" {
			if install.ProjectPath != "" && ctx.Cwd != "" &&
				pathWithin(ctx.Cwd, expandTilde(install.ProjectPath, ctx.Home)) {
				return root
			}
			continue
		}
		if userRoot == "" {
			userRoot = root
		}
	}
	return userRoot
}

func claudePluginName(root, key string) string {
	if data, found, ok := settingsFileIn(filepath.Join(root, ".claude-plugin"), "plugin.json"); found && ok {
		var manifest struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(data, &manifest) == nil && commandNamePattern.MatchString(manifest.Name) {
			return manifest.Name
		}
	}
	name, _, _ := strings.Cut(key, "@")
	return name
}

// claudePluginRootCommands reads a plugin's commands/ tree and its skills:
// the paths its manifest lists, else skills/.
func claudePluginRootCommands(root, plugin string, budget *int) ([]Command, []string, bool) {
	var commands []Command
	var suppressed []string
	truncated := false
	walkDirBudget(filepath.Join(root, "commands"), plugin, "plugin", &commands, &suppressed, budget, &truncated)

	skillPaths := []string{filepath.Join(root, "skills")}
	if configured := claudeManifestSkillPaths(root); configured != nil {
		skillPaths = configured
	}
	for _, path := range skillPaths {
		skillFile := filepath.Join(path, "SKILL.md")
		if info, err := os.Stat(skillFile); err == nil && info.Mode().IsRegular() {
			if *budget <= 0 {
				truncated = true
				break
			}
			*budget--
			if cmd, name := parseSkillEntry(skillFile, filepath.Base(path), plugin, "plugin"); cmd != nil {
				commands = append(commands, *cmd)
			} else if name != "" {
				suppressed = append(suppressed, name)
			}
			continue
		}
		cmds, supp, trunc := scanSkillDirBudget(path, "plugin", budget)
		for _, cmd := range cmds {
			cmd.Command = "/" + plugin + ":" + strings.TrimPrefix(cmd.Command, "/")
			commands = append(commands, cmd)
		}
		for _, name := range supp {
			suppressed = append(suppressed, "/"+plugin+":"+strings.TrimPrefix(name, "/"))
		}
		truncated = truncated || trunc
	}
	return commands, suppressed, truncated
}

// claudeManifestSkillPaths returns the skill paths plugin.json lists, kept
// inside the plugin, or nil when it lists none.
func claudeManifestSkillPaths(root string) []string {
	data, found, ok := settingsFileIn(filepath.Join(root, ".claude-plugin"), "plugin.json")
	if !found || !ok {
		return nil
	}
	var manifest claudePluginManifest
	if json.Unmarshal(data, &manifest) != nil || len(manifest.Skills) == 0 {
		return nil
	}
	var configured []string
	var single string
	if json.Unmarshal(manifest.Skills, &single) == nil {
		configured = []string{single}
	} else if json.Unmarshal(manifest.Skills, &configured) != nil {
		return nil
	}
	var paths []string
	for _, path := range configured {
		path = strings.TrimSpace(path)
		if path == "" || strings.HasPrefix(path, "~") {
			continue
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if pathWithin(path, root) {
			paths = append(paths, path)
		}
	}
	return paths
}
