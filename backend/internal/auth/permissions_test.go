package auth

import "testing"

func TestRolePermissionMatrix(t *testing.T) {
	cases := []struct {
		role string
		perm string
		want bool
	}{
		// Admin can do everything, including things no set enumerates.
		{RoleAdmin, "subscribers.view", true},
		{RoleAdmin, "managers.create", true},
		{RoleAdmin, "settings.edit", true},
		{RoleAdmin, "anything.invented", true},

		// Operator (Sara): subscriber view/create/edit, renew, disconnect;
		// no managers, no settings, no deletes.
		{RoleOperator, "subscribers.view", true},
		{RoleOperator, "subscribers.create", true},
		{RoleOperator, "subscribers.edit", true},
		{RoleOperator, PermRenew, true},
		{RoleOperator, PermDisconnect, true},
		{RoleOperator, "subscribers.delete", false},
		{RoleOperator, "managers.view", false},
		{RoleOperator, "managers.create", false},
		{RoleOperator, "settings.edit", false},

		// Agent (Hassan): scoped subscriber view, renew, reports view only.
		{RoleAgent, "subscribers.view", true},
		{RoleAgent, PermRenew, true},
		{RoleAgent, "reports.view", true},
		{RoleAgent, "subscribers.create", false},
		{RoleAgent, "subscribers.edit", false},
		{RoleAgent, PermDisconnect, false},
		{RoleAgent, "managers.view", false},

		// Unknown role is deny-by-default.
		{"ghost", "subscribers.view", false},
	}
	for _, c := range cases {
		m := &Manager{Role: c.role}
		if got := m.Can(c.perm); got != c.want {
			t.Errorf("role=%s perm=%s: got %v want %v", c.role, c.perm, got, c.want)
		}
	}
}

func TestScopeFilterOnlyForScoped(t *testing.T) {
	// Unscoped (admin) → nil.
	ctx := withManager(t.Context(), &Manager{ID: "a", Role: RoleAdmin, Scoped: false})
	if s := ScopeFilter(ctx); s != nil {
		t.Fatalf("unscoped manager must yield nil scope, got %+v", s)
	}
	// Scoped agent → own id.
	ctx = withManager(t.Context(), &Manager{ID: "hassan", Role: RoleAgent, Scoped: true})
	s := ScopeFilter(ctx)
	if s == nil || s.ManagerID != "hassan" {
		t.Fatalf("scoped manager must yield own id, got %+v", s)
	}
	// No manager in context → nil.
	if s := ScopeFilter(t.Context()); s != nil {
		t.Fatalf("empty context must yield nil scope, got %+v", s)
	}
}

// TestEffectiveSetOverridesFallback proves the embedded resolved set is
// authoritative over the builtin-role fallback: a Manager carrying an explicit
// Perms set is judged solely by it (wildcard grants all), while a Manager with
// a nil set falls back to the builtin role map (Phase-2 semantics preserved).
func TestEffectiveSetOverridesFallback(t *testing.T) {
	// Wildcard set → allow-all regardless of role text.
	admin := &Manager{Role: "", Perms: map[string]bool{permWildcard: true}}
	if !admin.Can("anything.invented") {
		t.Fatal("wildcard set must allow all")
	}
	// Explicit narrow set overrides the (privileged) role text.
	narrow := &Manager{Role: RoleAdmin, Perms: map[string]bool{"subscribers.view": true}}
	if narrow.Can("managers.create") {
		t.Fatal("embedded set must be authoritative over role fallback")
	}
	if !narrow.Can("subscribers.view") {
		t.Fatal("embedded set must grant its own permissions")
	}
	// Nil set → builtin-role fallback.
	op := &Manager{Role: RoleOperator}
	if !op.Can("subscribers.view") || op.Can("managers.view") {
		t.Fatal("nil set must fall back to builtin role map")
	}
}
