package update

import (
	"os"
	"regexp"
	"strings"
)

// canonicalRepository is the upstream project. A relay installed from a fork
// must check that fork's releases, not upstream's, or it would offer an update
// that silently replaces the fork with upstream.
const canonicalRepository = "0cv/herdr-mobile-relay"

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Repository is the GitHub owner/repo this relay updates from. The plugin
// build records HERDR_RELEASE_REPOSITORY into relay.env when it installs from
// a repository other than upstream, and the service wrapper exports it. A
// value that is not owner/repo is ignored rather than trusted: it would be
// spliced into URLs.
func Repository() string {
	value := strings.TrimSpace(os.Getenv("HERDR_RELEASE_REPOSITORY"))
	if value == "" || !repositoryPattern.MatchString(value) || strings.Contains(value, "..") {
		return canonicalRepository
	}
	return value
}

func apiBaseFor(repository string) string {
	return "https://api.github.com/repos/" + repository
}

func webBaseFor(repository string) string {
	return "https://github.com/" + repository
}

func assetsBaseFor(repository string) string {
	return webBaseFor(repository) + "/releases/download"
}

// assetsBase is where the worker downloads releases from. The update
// rehearsal points it at a local stand-in for GitHub; a real relay never sets
// the variable.
func assetsBase() string {
	if value := strings.TrimSpace(os.Getenv("HERDR_RELEASE_DOWNLOAD_BASE")); value != "" {
		return value
	}
	return assetsBaseFor(Repository())
}
