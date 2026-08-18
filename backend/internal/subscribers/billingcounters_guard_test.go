package subscribers

// Static guard for FR-134's frozen contract: "no reset path may ever write
// the lifetime billing counters" (v4-5 phase brief §"Binding rules", gate
// item 1). subscriber_billing_counters (migration 0602) has exactly one
// writer by design — internal/accounting's consumer, which increments it in
// lockstep with the usage_points/sessions rows it derives from — so any write
// statement against it OUTSIDE that package is exactly the bug this phase
// must never introduce, whether today (a reviewer catches it) or in a future
// change (this test catches it without needing a live database).
//
// This walks the whole backend module rather than just this package: a reset
// path could just as easily be added to billing (renewals) or radius (the
// limits worker) as to subscribers itself.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// billingCountersWriteRE matches an INSERT/UPDATE statement naming the
// lifetime table. It intentionally does NOT match a bare SELECT (reads are
// fine anywhere — the whole point is to show both counter families on the
// subscriber page) or a CREATE TABLE (the migration itself).
var billingCountersWriteRE = regexp.MustCompile(`(?i)(INSERT\s+INTO|UPDATE)\s+subscriber_billing_counters`)

// allowedBillingCountersWriter is the one package permitted to write the
// table — internal/accounting, matched by its path suffix so the test does
// not depend on the repo's absolute location.
const allowedBillingCountersWriter = "internal" + string(filepath.Separator) + "accounting"

func TestLifetimeBillingCountersHaveExactlyOneWriter(t *testing.T) {
	root := backendModuleRoot(t)

	var offenders []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "node_modules" || info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if strings.Contains(rel, allowedBillingCountersWriter) {
			return nil // the sanctioned writer
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if billingCountersWriteRE.Match(body) {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk backend module: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("subscriber_billing_counters (the LIFETIME billing table) must be written only by "+
			"internal/accounting; found a write statement in: %v — a reset/renewal path must touch "+
			"only subscriber_counters (the resettable table), never this one", offenders)
	}
}

// backendModuleRoot walks up from the current package directory to the
// directory containing go.mod (backend/), so the walk covers every package,
// not just this one.
func backendModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find backend module root (go.mod)")
		}
		dir = parent
	}
}
