package auth

// DB-backed suite for per-manager preferences (v2-6, FR-84.2, contracts
// C1-C3). Reuses the env/call/loginAdmin/uniq harness from db_test.go.

import (
	"net/http"
	"testing"
)

type prefsBody struct {
	Language          string                     `json:"language,omitempty"`
	Theme             string                     `json:"theme,omitempty"`
	Numerals          string                     `json:"numerals,omitempty"`
	LandingPage       string                     `json:"landing_page,omitempty"`
	TablePageSize     int                        `json:"table_page_size,omitempty"`
	NotificationPrefs map[string]map[string]bool `json:"notification_prefs,omitempty"`
}

// TestPreferencesNoRowDefaultsToZeroValue — gate item 2 (C1/C2): a manager
// with zero manager_preferences rows gets 200 with every field at its zero
// value, never 404.
func TestPreferencesNoRowDefaultsToZeroValue(t *testing.T) {
	e := setupRouter(t)
	admin := loginAdmin(t, e)

	r := call(t, e, "GET", "/api/v1/me/preferences", admin.AccessToken, nil)
	if r.status != http.StatusOK {
		t.Fatalf("get preferences = %d, want 200: %s", r.status, r.body)
	}
	var p prefsBody
	r.json(t, &p)
	if p.Language != "" || p.Theme != "" || p.Numerals != "" || p.LandingPage != "" || p.TablePageSize != 0 {
		t.Fatalf("fresh manager preferences not zero-value: %+v", p)
	}
}

// TestPreferencesCrossDeviceSeed — gate item 3 (AC-84a): a PUT'd preference is
// visible on an independent, later GET (no shared client state simulates a
// second device/session).
func TestPreferencesCrossDeviceSeed(t *testing.T) {
	e := setupRouter(t)
	sara := uniq("sara")
	admin := loginAdmin(t, e)
	r := call(t, e, "POST", "/api/v1/managers", admin.AccessToken, map[string]any{
		"username": sara, "password": "sara-pass-1", "role": "operator",
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create manager = %d: %s", r.status, r.body)
	}
	session1 := login(t, e, sara, "sara-pass-1")

	put := call(t, e, "PUT", "/api/v1/me/preferences", session1.AccessToken, prefsBody{
		Theme: "dark", Language: "ku",
	})
	if put.status != http.StatusOK {
		t.Fatalf("put preferences = %d: %s", put.status, put.body)
	}

	// A second, independent login simulates a second device/session — no
	// shared client state with session1.
	session2 := login(t, e, sara, "sara-pass-1")
	r = call(t, e, "GET", "/api/v1/me/preferences", session2.AccessToken, nil)
	if r.status != http.StatusOK {
		t.Fatalf("get preferences (session2) = %d: %s", r.status, r.body)
	}
	var p prefsBody
	r.json(t, &p)
	if p.Theme != "dark" || p.Language != "ku" {
		t.Fatalf("preferences did not follow the account across sessions: %+v", p)
	}
}

// TestPreferencesCrossManagerIsolation — gate item 4 (AC-84b): manager B's GET
// never reflects manager A's PUT; the endpoint has no id parameter, so a
// spoofed manager_id in the body cannot even attempt to redirect the write.
func TestPreferencesCrossManagerIsolation(t *testing.T) {
	e := setupRouter(t)
	admin := loginAdmin(t, e)

	nameA, nameB := uniq("mgra"), uniq("mgrb")
	for _, n := range []string{nameA, nameB} {
		r := call(t, e, "POST", "/api/v1/managers", admin.AccessToken, map[string]any{
			"username": n, "password": "pass-1234", "role": "operator",
		})
		if r.status != http.StatusCreated {
			t.Fatalf("create manager %s = %d: %s", n, r.status, r.body)
		}
	}
	a := login(t, e, nameA, "pass-1234")
	b := login(t, e, nameB, "pass-1234")

	// A spoofed manager_id/id field in the body is silently ignored — the row
	// written is always the caller's own, since the endpoint has no id param.
	put := call(t, e, "PUT", "/api/v1/me/preferences", a.AccessToken, map[string]any{
		"theme": "dark", "manager_id": b.Manager.ID, "id": b.Manager.ID,
	})
	if put.status != http.StatusOK {
		t.Fatalf("put preferences (A) = %d: %s", put.status, put.body)
	}

	r := call(t, e, "GET", "/api/v1/me/preferences", b.AccessToken, nil)
	if r.status != http.StatusOK {
		t.Fatalf("get preferences (B) = %d: %s", r.status, r.body)
	}
	var pb prefsBody
	r.json(t, &pb)
	if pb.Theme != "" {
		t.Fatalf("manager B's preferences reflect manager A's write (or the spoofed id worked): %+v", pb)
	}

	r = call(t, e, "GET", "/api/v1/me/preferences", a.AccessToken, nil)
	var pa prefsBody
	r.json(t, &pa)
	if pa.Theme != "dark" {
		t.Fatalf("manager A's own write did not stick: %+v", pa)
	}
}

// TestPreferencesValidationRejectsBadInput — gate item 6 (C3): an invalid
// theme/language/numerals value, an out-of-range table_page_size, and an
// unknown notification_prefs key each 422 with a field_errors entry; nothing
// is written on a rejected PUT.
func TestPreferencesValidationRejectsBadInput(t *testing.T) {
	e := setupRouter(t)
	sara := uniq("sara2")
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
		{"bad theme", map[string]any{"theme": "purple"}, "theme"},
		{"bad language", map[string]any{"language": "fr"}, "language"},
		{"bad numerals", map[string]any{"numerals": "roman"}, "numerals"},
		{"bad table_page_size", map[string]any{"table_page_size": 7}, "table_page_size"},
		{"unknown notification key", map[string]any{
			"notification_prefs": map[string]any{"typo_key": map[string]bool{"in_app": true, "push": false}},
		}, "notification_prefs.typo_key"},
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

	// Nothing was written by any of the rejected PUTs above.
	get := call(t, e, "GET", "/api/v1/me/preferences", session.AccessToken, nil)
	var p prefsBody
	get.json(t, &p)
	if p.Theme != "" || p.Language != "" || p.Numerals != "" || p.TablePageSize != 0 || len(p.NotificationPrefs) != 0 {
		t.Fatalf("a rejected PUT left a partial write: %+v", p)
	}
}
