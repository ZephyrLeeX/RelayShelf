package main

import (
	"fmt"
	"os"

	"github.com/ZephyrLeeX/RelayShelf/internal/uploads"
)

// failpointNames are the deterministic crash windows Phase 11 T125 drives
// through a real child process. They map onto the existing finalize failure
// hooks so no new production failure machinery is invented.
var failpointNames = map[string]struct {
	// hook selects the FinalizeFailureHooks field; "during-hash" is handled
	// by the dedicated hash hook.
	hook string
}{
	"during-hash":           {hook: "during-hash"},
	"after-pending":         {hook: "AfterPending"},
	"after-write":           {hook: "AfterWrite"},
	"before-sync":           {hook: "BeforeSync"},
	"before-rename":         {hook: "BeforeRename"},
	"after-rename":          {hook: "AfterRename"},
	"before-ready":          {hook: "BeforeReady"},
	"before-staging-delete": {hook: "BeforeStagingDelete"},
}

// crashHere returns the abrupt-exit function for the configured failpoint.
// The caller has already selected the hook position from failpointNames, so
// no further name matching happens here.
func crashHere() func() error {
	return func() error {
		// Abrupt termination, not a graceful unwind: the process must look
		// exactly like a crash to everything on disk and in PostgreSQL.
		os.Exit(70)
		return nil
	}
}

// testFailoutHooks builds finalize hooks from RELAYSHELF_TEST_FAILOUT. The
// failpoint requires both the explicit test-destructive confirmation and a
// recognized name, so a typo cannot silently pass qualification and a
// production environment cannot enable it by accident.
func testFailoutHooks() (duringHash func() error, hooks uploads.FinalizeFailureHooks, enabled bool, err error) {
	configured := os.Getenv("RELAYSHELF_TEST_FAILOUT")
	if configured == "" {
		return nil, uploads.FinalizeFailureHooks{}, false, nil
	}
	if os.Getenv("RELAYSHELF_TEST_DESTRUCTIVE") != "1" {
		return nil, uploads.FinalizeFailureHooks{}, false, fmt.Errorf("RELAYSHELF_TEST_FAILOUT requires RELAYSHELF_TEST_DESTRUCTIVE=1")
	}
	entry, ok := failpointNames[configured]
	if !ok {
		return nil, uploads.FinalizeFailureHooks{}, false, fmt.Errorf("unknown RELAYSHELF_TEST_FAILOUT %q", configured)
	}
	if entry.hook == "during-hash" {
		return crashHere(), uploads.FinalizeFailureHooks{}, true, nil
	}
	switch entry.hook {
	case "AfterPending":
		hooks.AfterPending = crashHere()
	case "AfterWrite":
		hooks.AfterWrite = crashHere()
	case "BeforeSync":
		hooks.BeforeSync = crashHere()
	case "BeforeRename":
		hooks.BeforeRename = crashHere()
	case "AfterRename":
		hooks.AfterRename = crashHere()
	case "BeforeReady":
		hooks.BeforeReady = crashHere()
	case "BeforeStagingDelete":
		hooks.BeforeStagingDelete = crashHere()
	default:
		return nil, uploads.FinalizeFailureHooks{}, false, fmt.Errorf("unwired failpoint %q", configured)
	}
	return nil, hooks, true, nil
}
