package auth

// Manager removal (owner request 2026-07-17): hard delete + disable. Gated on
// HIKRAD_TEST_DB_URL like the rest of the DB suite. The last-admin guard is
// deliberately not asserted here: the suite shares a database whose seeded
// admin (plus any managers earlier tests created) makes "no other admin
// exists" unreachable without destroying shared fixtures.

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestManagerDeleteRemovesAccount(t *testing.T) {
	e := setupRouter(t)
	admin := loginAdmin(t, e)
	m := createManager(t, e, admin, uniq("victim"), "operator", false)

	if r := call(t, e, "DELETE", "/api/v1/managers/"+m.ID, admin.AccessToken, nil); r.status != http.StatusNoContent {
		t.Fatalf("delete manager = %d: %s", r.status, r.body)
	}
	// Gone from the list and can no longer log in.
	r := call(t, e, "GET", "/api/v1/managers", admin.AccessToken, nil)
	if strings.Contains(string(r.body), m.ID) {
		t.Fatalf("deleted manager still listed")
	}
	lr := call(t, e, "POST", "/api/v1/auth/login", "", map[string]string{
		"username": m.Username, "password": "pw-" + m.Username,
	})
	if lr.status != http.StatusUnauthorized {
		t.Fatalf("login after delete = %d, want 401", lr.status)
	}
}

func TestManagerDeleteGuards(t *testing.T) {
	e := setupRouter(t)
	admin := loginAdmin(t, e)

	// Self-delete refused.
	if r := call(t, e, "DELETE", "/api/v1/managers/"+admin.Manager.ID, admin.AccessToken, nil); r.status != http.StatusConflict {
		t.Fatalf("self delete = %d, want 409: %s", r.status, r.body)
	} else if !strings.Contains(string(r.body), "cannot_remove_self") {
		t.Fatalf("self delete code = %s", r.body)
	}

	// Unknown id → 404 (both malformed and valid-but-absent).
	if r := call(t, e, "DELETE", "/api/v1/managers/not-a-uuid", admin.AccessToken, nil); r.status != http.StatusNotFound {
		t.Fatalf("bad uuid delete = %d, want 404", r.status)
	}
	if r := call(t, e, "DELETE", "/api/v1/managers/00000000-0000-0000-0000-000000000001", admin.AccessToken, nil); r.status != http.StatusNotFound {
		t.Fatalf("absent delete = %d, want 404", r.status)
	}

	// A manager with ledger history is undeletable: the ledger's append-only
	// trigger blocks the FK SET NULL, mapped to 409 has_history.
	m := createManager(t, e, admin, uniq("agent"), "agent", false)
	if _, err := e.db.Exec(context.Background(),
		`INSERT INTO ledger_transactions (type, amount, actor_manager_id, source, note)
		 VALUES ('topup', 1000, $1::uuid, 'panel', 'removal-test')`, m.ID); err != nil {
		t.Fatalf("plant ledger row: %v", err)
	}
	r := call(t, e, "DELETE", "/api/v1/managers/"+m.ID, admin.AccessToken, nil)
	if r.status != http.StatusConflict || !strings.Contains(string(r.body), "has_history") {
		t.Fatalf("delete with ledger history = %d %s, want 409 has_history", r.status, r.body)
	}
}

func TestManagerDisableBlocksLoginAndRefresh(t *testing.T) {
	e := setupRouter(t)
	admin := loginAdmin(t, e)
	m := createManager(t, e, admin, uniq("todisable"), "operator", false)
	sess := login(t, e, m.Username, "pw-"+m.Username)

	// Admins cannot disable themselves.
	if r := call(t, e, "PUT", "/api/v1/managers/"+admin.Manager.ID, admin.AccessToken,
		map[string]any{"disabled": true}); r.status != http.StatusConflict {
		t.Fatalf("self disable = %d, want 409: %s", r.status, r.body)
	}

	r := call(t, e, "PUT", "/api/v1/managers/"+m.ID, admin.AccessToken, map[string]any{"disabled": true})
	if r.status != http.StatusOK || !strings.Contains(string(r.body), `"disabled":true`) {
		t.Fatalf("disable = %d: %s", r.status, r.body)
	}

	// Login is refused with an explicit code, and the pre-disable refresh
	// token is dead (sessions were revoked).
	lr := call(t, e, "POST", "/api/v1/auth/login", "", map[string]string{
		"username": m.Username, "password": "pw-" + m.Username,
	})
	if lr.status != http.StatusForbidden || !strings.Contains(string(lr.body), "account_disabled") {
		t.Fatalf("login while disabled = %d: %s", lr.status, lr.body)
	}
	rr := call(t, e, "POST", "/api/v1/auth/refresh", "", map[string]string{"refresh_token": sess.RefreshToken})
	if rr.status != http.StatusUnauthorized {
		t.Fatalf("refresh while disabled = %d, want 401: %s", rr.status, rr.body)
	}

	// Re-enable restores login.
	if r := call(t, e, "PUT", "/api/v1/managers/"+m.ID, admin.AccessToken, map[string]any{"disabled": false}); r.status != http.StatusOK {
		t.Fatalf("enable = %d: %s", r.status, r.body)
	}
	login(t, e, m.Username, "pw-"+m.Username)
}
