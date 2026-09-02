package update

import "testing"

func TestRepositoryDefaultsToUpstream(t *testing.T) {
	t.Setenv("HERDR_RELEASE_REPOSITORY", "")
	if got := Repository(); got != canonicalRepository {
		t.Fatalf("Repository = %q", got)
	}
}

func TestRepositoryFollowsTheForkItWasInstalledFrom(t *testing.T) {
	t.Setenv("HERDR_RELEASE_REPOSITORY", "devalexsegault/herdr-mobile-app")
	if got := Repository(); got != "devalexsegault/herdr-mobile-app" {
		t.Fatalf("Repository = %q", got)
	}
	if got := apiBaseFor(Repository()); got != "https://api.github.com/repos/devalexsegault/herdr-mobile-app" {
		t.Fatalf("api base = %q", got)
	}
	if got := assetsBaseFor(Repository()); got != "https://github.com/devalexsegault/herdr-mobile-app/releases/download" {
		t.Fatalf("assets base = %q", got)
	}
}

// The value is spliced into URLs, so anything that is not owner/repo falls
// back to upstream instead of being trusted.
func TestRepositoryRejectsAnythingButOwnerSlashRepo(t *testing.T) {
	for _, bad := range []string{"owner", "owner/repo/extra", "../x/y", "https://github.com/a/b", "a b/c"} {
		t.Setenv("HERDR_RELEASE_REPOSITORY", bad)
		if got := Repository(); got != canonicalRepository {
			t.Errorf("Repository(%q) = %q, want upstream", bad, got)
		}
	}
}
