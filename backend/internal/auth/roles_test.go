package auth

import (
	"context"
	"testing"
)

func mgrCtx(m *Manager) context.Context {
	return withManager(context.Background(), m)
}

func TestEscalationCheck(t *testing.T) {
	// Wildcard editor may grant anything.
	admin := mgrCtx(&Manager{Perms: map[string]bool{permWildcard: true}})
	if d := escalationCheck(admin, []string{"managers.create", "settings.edit"}); d != "" {
		t.Fatalf("admin should grant anything, denied %q", d)
	}
	// Narrow editor may only grant what they hold.
	narrow := mgrCtx(&Manager{Perms: map[string]bool{"subscribers.view": true, "subscribers.edit": true}})
	if d := escalationCheck(narrow, []string{"subscribers.view"}); d != "" {
		t.Fatalf("should be allowed to grant held permission, denied %q", d)
	}
	if d := escalationCheck(narrow, []string{"subscribers.view", "managers.create"}); d != "managers.create" {
		t.Fatalf("should deny escalation to managers.create, got %q", d)
	}
	// No manager in context → deny any non-empty grant.
	if d := escalationCheck(context.Background(), []string{"subscribers.view"}); d != "subscribers.view" {
		t.Fatalf("no-actor context must deny, got %q", d)
	}
}

func TestDiffAdded(t *testing.T) {
	added := diffAdded([]string{"a", "b"}, []string{"b", "c", "d"})
	want := map[string]bool{"c": true, "d": true}
	if len(added) != 2 {
		t.Fatalf("added=%v want c,d", added)
	}
	for _, p := range added {
		if !want[p] {
			t.Fatalf("unexpected added permission %q", p)
		}
	}
	if len(diffAdded([]string{"a"}, []string{"a"})) != 0 {
		t.Fatal("no additions expected")
	}
}

func TestInvalidPermissions(t *testing.T) {
	if bad := invalidPermissions([]string{"subscribers.view", PermRenew}); bad != "" {
		t.Fatalf("catalog permissions should be valid, got %q", bad)
	}
	if bad := invalidPermissions([]string{"subscribers.view", "made.up"}); bad != "made.up" {
		t.Fatalf("expected made.up flagged, got %q", bad)
	}
	// The wildcard is intentionally not hand-assignable via the catalog.
	if bad := invalidPermissions([]string{permWildcard}); bad != permWildcard {
		t.Fatalf("wildcard must not be catalog-assignable, got %q", bad)
	}
}
