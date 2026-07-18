package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// State is the on-disk record (C2) that survives a daemon restart — the
// in-memory *run tracks a live update, but `status`/`rollback-status` must
// still answer correctly after the daemon process itself was restarted
// (gate item 5), so every stage transition is mirrored here.
type State struct {
	Stage       string     `json:"stage"` // idle|backup|apply|health_check|rolling_back|done|rolled_back
	LockOwner   string     `json:"lock_owner,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	LastAction  string     `json:"last_action,omitempty"`
	LastResult  string     `json:"last_result,omitempty"` // success|rolled_back|failed
	LastVersion string     `json:"last_version,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	// PreviousVersion/TargetVersion are not part of C2's frozen response
	// shapes — they're the reconciliation seam a restarted daemon uses to
	// resolve a run it never observed the end of (see reconcileIfStale in
	// run.go): if the running system's VersionFile matches TargetVersion the
	// run succeeded, if it matches PreviousVersion it rolled back, otherwise
	// the outcome is genuinely ambiguous and recorded as "failed".
	PreviousVersion string `json:"previous_version,omitempty"`
	TargetVersion   string `json:"target_version,omitempty"`
}

func isTerminalStage(stage string) bool {
	switch stage {
	case "", "idle", "done", "rolled_back":
		return true
	}
	return false
}

func loadState(path string) State {
	b, err := os.ReadFile(path)
	if err != nil {
		return State{Stage: "idle"}
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return State{Stage: "idle"}
	}
	return s
}

// saveState writes atomically (tmp file + rename) so a crash mid-write never
// leaves a torn/unparseable state.json behind.
func saveState(path string, s State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o770); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func timePtr(t time.Time) *time.Time { return &t }
