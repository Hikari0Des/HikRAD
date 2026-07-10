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

func TestValidRole(t *testing.T) {
	for _, r := range []string{RoleAdmin, RoleOperator, RoleAgent} {
		if !validRole(r) {
			t.Errorf("%s should be valid", r)
		}
	}
	for _, r := range []string{"", "superuser", "Admin"} {
		if validRole(r) {
			t.Errorf("%s should be invalid", r)
		}
	}
}
