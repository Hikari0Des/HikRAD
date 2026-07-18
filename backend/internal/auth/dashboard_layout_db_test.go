package auth

// v2-10 gate tests (FR-90, contract C2). Reuses the env/call/loginAdmin/uniq
// harness from db_test.go and prefsBody from preferences_db_test.go.

import (
	"net/http"
	"testing"
)

// TestPreferencesDashboardLayoutCrossDeviceSeed — gate item 4: a PUT'd
// dashboard_layout is visible on an independent, later GET (simulated second
// device/session, mirroring TestPreferencesCrossDeviceSeed's pattern).
func TestPreferencesDashboardLayoutCrossDeviceSeed(t *testing.T) {
	e := setupRouter(t)
	sara := uniq("sara_dash")
	admin := loginAdmin(t, e)
	r := call(t, e, "POST", "/api/v1/managers", admin.AccessToken, map[string]any{
		"username": sara, "password": "sara-pass-1", "role": "operator",
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create manager = %d: %s", r.status, r.body)
	}
	session1 := login(t, e, sara, "sara-pass-1")

	layout := DashboardLayout{Widgets: []DashboardWidgetRef{
		{ID: "my-balance", Size: "1x"},
		{ID: "nas-health", Size: "2x"},
	}}
	put := call(t, e, "PUT", "/api/v1/me/preferences", session1.AccessToken, prefsBody{DashboardLayout: &layout})
	if put.status != http.StatusOK {
		t.Fatalf("put preferences = %d: %s", put.status, put.body)
	}

	session2 := login(t, e, sara, "sara-pass-1")
	get := call(t, e, "GET", "/api/v1/me/preferences", session2.AccessToken, nil)
	if get.status != http.StatusOK {
		t.Fatalf("get preferences (session2) = %d: %s", get.status, get.body)
	}
	var p prefsBody
	get.json(t, &p)
	if p.DashboardLayout == nil || len(p.DashboardLayout.Widgets) != 2 {
		t.Fatalf("dashboard_layout did not follow the account across sessions: %+v", p)
	}
	if p.DashboardLayout.Widgets[0].ID != "my-balance" || p.DashboardLayout.Widgets[1].ID != "nas-health" {
		t.Fatalf("dashboard_layout widgets/order not preserved: %+v", p.DashboardLayout.Widgets)
	}
	if p.DashboardLayout.Widgets[1].Size != "2x" {
		t.Fatalf("dashboard_layout widget size not preserved: %+v", p.DashboardLayout.Widgets[1])
	}
}

// TestPreferencesDashboardLayoutResetToDefault — gate item 6: PUT with
// dashboard_layout omitted clears a previously-saved layout back to default
// (nil) — same full-document-replace mechanism as every other preference
// field, no new endpoint.
func TestPreferencesDashboardLayoutResetToDefault(t *testing.T) {
	e := setupRouter(t)
	sara := uniq("sara_reset")
	admin := loginAdmin(t, e)
	r := call(t, e, "POST", "/api/v1/managers", admin.AccessToken, map[string]any{
		"username": sara, "password": "sara-pass-1", "role": "operator",
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create manager = %d: %s", r.status, r.body)
	}
	session := login(t, e, sara, "sara-pass-1")

	layout := DashboardLayout{Widgets: []DashboardWidgetRef{{ID: "my-balance", Size: "1x"}}}
	put := call(t, e, "PUT", "/api/v1/me/preferences", session.AccessToken, prefsBody{
		Theme: "dark", DashboardLayout: &layout,
	})
	if put.status != http.StatusOK {
		t.Fatalf("put preferences = %d: %s", put.status, put.body)
	}

	// Reset: PUT again with dashboard_layout omitted (the struct's omitempty
	// tag drops it entirely when nil) but theme still set, proving the reset
	// is scoped to the one field, not a wipe of the whole document.
	reset := call(t, e, "PUT", "/api/v1/me/preferences", session.AccessToken, prefsBody{Theme: "dark"})
	if reset.status != http.StatusOK {
		t.Fatalf("reset put = %d: %s", reset.status, reset.body)
	}

	get := call(t, e, "GET", "/api/v1/me/preferences", session.AccessToken, nil)
	var p prefsBody
	get.json(t, &p)
	if p.DashboardLayout != nil {
		t.Fatalf("dashboard_layout did not reset to default (nil): %+v", p.DashboardLayout)
	}
	if p.Theme != "dark" {
		t.Fatalf("reset wiped an unrelated field: theme = %q, want dark", p.Theme)
	}
}

// TestPreferencesDashboardLayoutValidation — gate item 7: an unknown widget
// id or an invalid size 422s naming the offending path; nothing is written.
func TestPreferencesDashboardLayoutValidation(t *testing.T) {
	e := setupRouter(t)
	sara := uniq("sara_val")
	admin := loginAdmin(t, e)
	r := call(t, e, "POST", "/api/v1/managers", admin.AccessToken, map[string]any{
		"username": sara, "password": "sara-pass-1", "role": "operator",
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create manager = %d: %s", r.status, r.body)
	}
	session := login(t, e, sara, "sara-pass-1")

	cases := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{
			"unknown widget id",
			map[string]any{"dashboard_layout": map[string]any{
				"widgets": []map[string]any{{"id": "not-a-real-widget", "size": "1x"}},
			}},
			"dashboard_layout.widgets.0.id",
		},
		{
			"invalid size",
			map[string]any{"dashboard_layout": map[string]any{
				"widgets": []map[string]any{{"id": "my-balance", "size": "3x"}},
			}},
			"dashboard_layout.widgets.0.size",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := call(t, e, "PUT", "/api/v1/me/preferences", session.AccessToken, c.body)
			if r.status != http.StatusUnprocessableEntity {
				t.Fatalf("%s: status = %d, want 422: %s", c.name, r.status, r.body)
			}
			var env struct {
				Error struct {
					FieldErrors []struct {
						Field string `json:"field"`
					} `json:"field_errors"`
				} `json:"error"`
			}
			r.json(t, &env)
			found := false
			for _, fe := range env.Error.FieldErrors {
				if fe.Field == c.field {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s: no field_errors entry naming %q: %s", c.name, c.field, r.body)
			}
		})
	}

	get := call(t, e, "GET", "/api/v1/me/preferences", session.AccessToken, nil)
	var p prefsBody
	get.json(t, &p)
	if p.DashboardLayout != nil {
		t.Fatalf("a rejected PUT left a partial dashboard_layout write: %+v", p.DashboardLayout)
	}
}
