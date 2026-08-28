package buildinfo

import (
	"runtime/debug"
	"testing"
)

// withVars overrides the ldflags-populated variables and restores them after
// the test, so each case can observe a different injection state.
func withVars(t *testing.T, version, gitCommit, buildTime string) {
	t.Helper()
	original := Info{Version: Version, GitCommit: GitCommit, BuildTime: BuildTime}
	Version, GitCommit, BuildTime = version, gitCommit, buildTime
	t.Cleanup(func() {
		Version, GitCommit, BuildTime = original.Version, original.GitCommit, original.BuildTime
	})
}

func vcsSettings(t *testing.T) (revision, committedAt string) {
	t.Helper()
	build, ok := debug.ReadBuildInfo()
	if !ok {
		return "", ""
	}
	for _, setting := range build.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.time":
			committedAt = setting.Value
		}
	}
	return revision, committedAt
}

func TestExplicitLdflagsValuesWinOverBuildInfoFallback(t *testing.T) {
	withVars(t, "9.9.9-review", "0123456789abcdef0123", "2026-08-28T10:11:12Z")
	info := Current()
	if info.Version != "9.9.9-review" || info.GitCommit != "0123456789abcdef0123" || info.BuildTime != "2026-08-28T10:11:12Z" {
		t.Fatalf("explicitly injected metadata was overridden: %+v", info)
	}
}

func TestUnsetVariablesFallBackToBuildInfoOrUnknown(t *testing.T) {
	withVars(t, "", "", "")
	info := Current()
	revision, _ := vcsSettings(t)
	if revision != "" {
		if info.GitCommit != revision {
			t.Fatalf("git commit %q does not match vcs.revision %q", info.GitCommit, revision)
		}
	} else if info.GitCommit != "unknown" {
		t.Fatalf("unstamped build must report unknown commit, got %q", info.GitCommit)
	}
	_, committedAt := vcsSettings(t)
	if committedAt != "" {
		if info.BuildTime != committedAt {
			t.Fatalf("build time %q does not match vcs.time %q", info.BuildTime, committedAt)
		}
	} else if info.BuildTime != "unknown" {
		t.Fatalf("unstamped build must report unknown build time, got %q", info.BuildTime)
	}
	if info.Version == "" {
		t.Fatal("version must never be empty")
	}
}

func TestPartialInjectionFallsBackPerField(t *testing.T) {
	withVars(t, "3.2.1-partial", "", "")
	info := Current()
	if info.Version != "3.2.1-partial" {
		t.Fatalf("injected version not honored: %+v", info)
	}
	revision, _ := vcsSettings(t)
	if revision != "" {
		if info.GitCommit != revision {
			t.Fatalf("unset git commit must use fallback %q, got %q", revision, info.GitCommit)
		}
	} else if info.GitCommit != "unknown" {
		t.Fatalf("unset git commit must be unknown, got %q", info.GitCommit)
	}
	if info.BuildTime == "" {
		t.Fatal("build time must never be empty")
	}
}
