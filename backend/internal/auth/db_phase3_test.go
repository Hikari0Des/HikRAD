package auth

// Phase-3 DB-backed suite (gated on HIKRAD_TEST_DB_URL): editable roles + matrix
// resolution with overrides, the privilege-escalation guard, full TOTP flows
// (enroll/verify/skew/backup/disable/admin-reset), IP-allowlist enforcement
// incl. XFF, and the audit viewer filters + CSV export. Reuses the helpers in
// db_test.go (same package). Every test self-scopes with unique usernames.

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// createManager is a small helper: admin creates a manager on the named role.
func createManager(t *testing.T, e env, admin loginOut, username, role string, scoped bool) managerView {
	t.Helper()
	r := call(t, e, "POST", "/api/v1/managers", admin.AccessToken, map[string]any{
		"username": username, "password": "pw-" + username, "role": role, "scoped": scoped,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create manager %s = %d: %s", username, r.status, r.body)
	}
	var v managerView
	r.json(t, &v)
	return v
}

func TestRoleMatrixResolutionWithOverrides(t *testing.T) {
	e := setupRouter(t)
	admin := loginAdmin(t, e)

	// A custom role with a single permission.
	roleName := uniq("role")
	r := call(t, e, "POST", "/api/v1/roles", admin.AccessToken, map[string]any{
		"name": roleName, "description": "test", "permissions": []string{"subscribers.view"},
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create role = %d: %s", r.status, r.body)
	}
	m := createManager(t, e, admin, uniq("user"), roleName, false)

	// Effective set is exactly the role's permission.
	eff := effectivePerms(t, e, admin, m.ID)
	if !eff["subscribers.view"] || eff["profiles.view"] {
		t.Fatalf("unexpected effective set: %v", eff)
	}

	// Grant an override; the effective set gains it.
	r = call(t, e, "PUT", "/api/v1/managers/"+m.ID+"/permissions", admin.AccessToken, map[string]any{
		"overrides": []map[string]any{{"permission": "profiles.view", "granted": true}},
	})
	if r.status != http.StatusOK {
		t.Fatalf("put overrides = %d: %s", r.status, r.body)
	}
	eff = effectivePerms(t, e, admin, m.ID)
	if !eff["profiles.view"] {
		t.Fatalf("override not reflected in effective set: %v", eff)
	}

	// Revoke a role-granted permission via override.
	r = call(t, e, "PUT", "/api/v1/managers/"+m.ID+"/permissions", admin.AccessToken, map[string]any{
		"overrides": []map[string]any{{"permission": "subscribers.view", "granted": false}},
	})
	if r.status != http.StatusOK {
		t.Fatalf("put revoke override = %d: %s", r.status, r.body)
	}
	eff = effectivePerms(t, e, admin, m.ID)
	if eff["subscribers.view"] {
		t.Fatalf("revoking override still shows permission: %v", eff)
	}
}

func effectivePerms(t *testing.T, e env, admin loginOut, managerID string) map[string]bool {
	t.Helper()
	r := call(t, e, "GET", "/api/v1/managers/"+managerID+"/permissions", admin.AccessToken, nil)
	if r.status != http.StatusOK {
		t.Fatalf("get permissions = %d: %s", r.status, r.body)
	}
	var body struct {
		Effective []string `json:"effective"`
	}
	r.json(t, &body)
	set := map[string]bool{}
	for _, p := range body.Effective {
		set[p] = true
	}
	return set
}

func TestRoleEditTakesEffectAfterRefresh(t *testing.T) {
	e := setupRouter(t)
	admin := loginAdmin(t, e)
	roleName := uniq("role")
	r := call(t, e, "POST", "/api/v1/roles", admin.AccessToken, map[string]any{
		"name": roleName, "permissions": []string{"subscribers.view"},
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create role = %d: %s", r.status, r.body)
	}
	var role roleView
	r.json(t, &role)
	m := createManager(t, e, admin, uniq("user"), roleName, false)

	sess := login(t, e, m.Username, "pw-"+m.Username)
	// Without managers.view the manager is denied.
	if rr := call(t, e, "GET", "/api/v1/managers", sess.AccessToken, nil); rr.status != http.StatusForbidden {
		t.Fatalf("expected 403 before grant, got %d", rr.status)
	}
	// Grant managers.view on the role, then refresh: the new token carries it.
	if rr := call(t, e, "PUT", "/api/v1/roles/"+role.ID, admin.AccessToken, map[string]any{
		"permissions": []string{"subscribers.view", "managers.view"},
	}); rr.status != http.StatusOK {
		t.Fatalf("update role = %d: %s", rr.status, rr.body)
	}
	rr := call(t, e, "POST", "/api/v1/auth/refresh", "", map[string]string{"refresh_token": sess.RefreshToken})
	if rr.status != http.StatusOK {
		t.Fatalf("refresh = %d: %s", rr.status, rr.body)
	}
	var refreshed loginOut
	rr.json(t, &refreshed)
	if got := call(t, e, "GET", "/api/v1/managers", refreshed.AccessToken, nil); got.status != http.StatusOK {
		t.Fatalf("expected 200 after grant+refresh, got %d: %s", got.status, got.body)
	}
}

func TestRoleDeleteBlockedWhenInUse(t *testing.T) {
	e := setupRouter(t)
	admin := loginAdmin(t, e)
	roleName := uniq("role")
	r := call(t, e, "POST", "/api/v1/roles", admin.AccessToken, map[string]any{
		"name": roleName, "permissions": []string{"subscribers.view"},
	})
	var role roleView
	r.json(t, &role)
	createManager(t, e, admin, uniq("user"), roleName, false)

	if rr := call(t, e, "DELETE", "/api/v1/roles/"+role.ID, admin.AccessToken, nil); rr.status != http.StatusConflict {
		t.Fatalf("in-use role delete = %d, want 409: %s", rr.status, rr.body)
	}
	// Builtin roles cannot be deleted either.
	list := call(t, e, "GET", "/api/v1/roles", admin.AccessToken, nil)
	var roles struct {
		Items []roleView `json:"items"`
	}
	list.json(t, &roles)
	var adminRoleID string
	for _, rv := range roles.Items {
		if rv.Name == "admin" {
			adminRoleID = rv.ID
		}
	}
	if adminRoleID == "" {
		t.Fatal("builtin admin role not listed")
	}
	if rr := call(t, e, "DELETE", "/api/v1/roles/"+adminRoleID, admin.AccessToken, nil); rr.status != http.StatusConflict {
		t.Fatalf("builtin role delete = %d, want 409", rr.status)
	}
}

func TestRoleRejectsUnknownPermission(t *testing.T) {
	e := setupRouter(t)
	admin := loginAdmin(t, e)
	r := call(t, e, "POST", "/api/v1/roles", admin.AccessToken, map[string]any{
		"name": uniq("role"), "permissions": []string{"made.up.permission"},
	})
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("unknown permission = %d, want 422: %s", r.status, r.body)
	}
}

func TestTOTPFullFlow(t *testing.T) {
	e := setupRouter(t)
	admin := loginAdmin(t, e)
	m := createManager(t, e, admin, uniq("mfa"), "operator", false)
	sess := login(t, e, m.Username, "pw-"+m.Username)

	// Enroll → returns a base32 secret + otpauth URI.
	r := call(t, e, "POST", "/api/v1/auth/totp/enroll", sess.AccessToken, map[string]any{})
	if r.status != http.StatusOK {
		t.Fatalf("enroll = %d: %s", r.status, r.body)
	}
	var enroll enrollResponse
	r.json(t, &enroll)
	if enroll.Secret == "" || !strings.HasPrefix(enroll.OTPAuthURI, "otpauth://") {
		t.Fatalf("bad enroll response: %s", r.body)
	}

	// Verify with a live code → activates, returns 10 backup codes.
	code, _ := totpCodeAt(enroll.Secret, time.Now())
	r = call(t, e, "POST", "/api/v1/auth/totp/verify", sess.AccessToken, map[string]any{"code": code})
	if r.status != http.StatusOK {
		t.Fatalf("verify = %d: %s", r.status, r.body)
	}
	var vresp struct {
		BackupCodes []string `json:"backup_codes"`
	}
	r.json(t, &vresp)
	if len(vresp.BackupCodes) != backupCodeCount {
		t.Fatalf("got %d backup codes, want %d", len(vresp.BackupCodes), backupCodeCount)
	}

	// Login now demands a code.
	pw := "pw-" + m.Username
	if rr := call(t, e, "POST", "/api/v1/auth/login", "", map[string]string{"username": m.Username, "password": pw}); rr.status != http.StatusUnauthorized || !strings.Contains(string(rr.body), "totp_required") {
		t.Fatalf("login without code = %d: %s", rr.status, rr.body)
	}
	// Correct TOTP code logs in.
	code, _ = totpCodeAt(enroll.Secret, time.Now())
	rr := call(t, e, "POST", "/api/v1/auth/login", "", map[string]string{"username": m.Username, "password": pw, "totp_code": code})
	if rr.status != http.StatusOK {
		t.Fatalf("login with totp = %d: %s", rr.status, rr.body)
	}

	// A backup code is single-use.
	bc := vresp.BackupCodes[0]
	if rr := call(t, e, "POST", "/api/v1/auth/login", "", map[string]string{"username": m.Username, "password": pw, "totp_code": bc}); rr.status != http.StatusOK {
		t.Fatalf("login with backup code = %d: %s", rr.status, rr.body)
	}
	if rr := call(t, e, "POST", "/api/v1/auth/login", "", map[string]string{"username": m.Username, "password": pw, "totp_code": bc}); rr.status == http.StatusOK {
		t.Fatal("backup code reuse must be rejected")
	}

	// Disable with password + a live code.
	sess2 := loginTOTP(t, e, m.Username, pw, enroll.Secret)
	code, _ = totpCodeAt(enroll.Secret, time.Now())
	if rr := call(t, e, "POST", "/api/v1/auth/totp/disable", sess2.AccessToken, map[string]any{"password": pw, "code": code}); rr.status != http.StatusNoContent {
		t.Fatalf("disable = %d: %s", rr.status, rr.body)
	}
	// After disable, plain login works again.
	login(t, e, m.Username, pw)
}

// loginTOTP logs in supplying a freshly-computed code for a 2FA-enabled account.
func loginTOTP(t *testing.T, e env, user, pass, secret string) loginOut {
	t.Helper()
	code, _ := totpCodeAt(secret, time.Now())
	r := call(t, e, "POST", "/api/v1/auth/login", "", map[string]string{"username": user, "password": pass, "totp_code": code})
	if r.status != http.StatusOK {
		t.Fatalf("totp login %s = %d: %s", user, r.status, r.body)
	}
	var out loginOut
	r.json(t, &out)
	return out
}

func TestAdminResetTOTP(t *testing.T) {
	e := setupRouter(t)
	admin := loginAdmin(t, e)
	m := createManager(t, e, admin, uniq("locked"), "operator", false)
	sess := login(t, e, m.Username, "pw-"+m.Username)

	// Enroll + activate.
	r := call(t, e, "POST", "/api/v1/auth/totp/enroll", sess.AccessToken, map[string]any{})
	var enroll enrollResponse
	r.json(t, &enroll)
	code, _ := totpCodeAt(enroll.Secret, time.Now())
	if rr := call(t, e, "POST", "/api/v1/auth/totp/verify", sess.AccessToken, map[string]any{"code": code}); rr.status != http.StatusOK {
		t.Fatalf("verify = %d: %s", rr.status, rr.body)
	}

	// Admin resets the manager's 2FA → plain login works, audited.
	if rr := call(t, e, "POST", "/api/v1/managers/"+m.ID+"/totp/reset", admin.AccessToken, nil); rr.status != http.StatusNoContent {
		t.Fatalf("admin reset = %d: %s", rr.status, rr.body)
	}
	login(t, e, m.Username, "pw-"+m.Username)
	if auditCountByEntity(t, e.db, "manager", m.ID, "managers.totp_reset") < 1 {
		t.Fatal("admin totp reset not audited")
	}
}

func TestIPAllowlistBlocksLoginAndXFF(t *testing.T) {
	e := setupRouter(t)
	admin := loginAdmin(t, e)
	m := createManager(t, e, admin, uniq("netlock"), "operator", false)
	pw := "pw-" + m.Username

	// Restrict to a range that excludes the test's 10.x XFF.
	if rr := call(t, e, "PUT", "/api/v1/managers/"+m.ID+"/ip-allowlist", admin.AccessToken, map[string]any{
		"entries": []map[string]any{{"cidr": "203.0.113.0/24"}},
	}); rr.status != http.StatusOK {
		t.Fatalf("set allowlist = %d: %s", rr.status, rr.body)
	}
	if rr := call(t, e, "POST", "/api/v1/auth/login", "", map[string]string{"username": m.Username, "password": pw}); rr.status != http.StatusForbidden || !strings.Contains(string(rr.body), "ip_not_allowed") {
		t.Fatalf("login from disallowed IP = %d: %s", rr.status, rr.body)
	}
	// A request from an allowed XFF succeeds.
	if rr := loginFromIP(t, e, m.Username, pw, "203.0.113.7"); rr.status != http.StatusOK {
		t.Fatalf("login from allowed XFF = %d: %s", rr.status, rr.body)
	}
	// Widen to include the default XFF → the standard helper logs in.
	if rr := call(t, e, "PUT", "/api/v1/managers/"+m.ID+"/ip-allowlist", admin.AccessToken, map[string]any{
		"entries": []map[string]any{{"cidr": "10.0.0.0/8"}, {"cidr": "203.0.113.0/24"}},
	}); rr.status != http.StatusOK {
		t.Fatalf("widen allowlist = %d: %s", rr.status, rr.body)
	}
	login(t, e, m.Username, pw)
}

// loginFromIP issues a login with an explicit X-Forwarded-For (overriding the
// env default) to exercise per-XFF allowlist enforcement.
func loginFromIP(t *testing.T, e env, user, pass, xff string) apiResp {
	t.Helper()
	body := strings.NewReader(`{"username":"` + user + `","password":"` + pass + `"}`)
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", xff)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return apiResp{status: resp.StatusCode, body: raw}
}

func TestAuditViewerFiltersAndExport(t *testing.T) {
	e := setupRouter(t)
	admin := loginAdmin(t, e)
	m := createManager(t, e, admin, uniq("audited"), "operator", false)

	// Filter by action + entity: the create is present with a localizable summary.
	r := call(t, e, "GET", "/api/v1/audit-log?entity_type=manager&entity_id="+m.ID+"&action=managers.create", admin.AccessToken, nil)
	if r.status != http.StatusOK {
		t.Fatalf("audit filter = %d: %s", r.status, r.body)
	}
	var list struct {
		Items []auditEntry `json:"items"`
	}
	r.json(t, &list)
	if len(list.Items) == 0 {
		t.Fatalf("no audit rows for managers.create: %s", r.body)
	}
	for _, it := range list.Items {
		if it.Action != "managers.create" || it.EntityID != m.ID {
			t.Fatalf("filter leaked row: %+v", it)
		}
		if it.SummaryKey != "audit.action.managers.create" {
			t.Fatalf("bad summary key: %q", it.SummaryKey)
		}
	}

	// Date-range filter with an impossible future window returns nothing.
	future := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	r = call(t, e, "GET", "/api/v1/audit-log?from="+future, admin.AccessToken, nil)
	r.json(t, &list)
	if len(list.Items) != 0 {
		t.Fatalf("future from-filter should be empty, got %d", len(list.Items))
	}

	// CSV export is gated on `export` (admin holds it) and returns CSV.
	r = call(t, e, "GET", "/api/v1/audit-log/export?entity_id="+m.ID, admin.AccessToken, nil)
	if r.status != http.StatusOK {
		t.Fatalf("export = %d: %s", r.status, r.body)
	}
	if !strings.HasPrefix(string(r.body), "id,at,actor_id,action") {
		t.Fatalf("unexpected CSV header: %s", firstLine(r.body))
	}
}

func firstLine(b []byte) string {
	if i := strings.IndexByte(string(b), '\n'); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

func TestRequire2FASettingBlocksLoginUntilEnrolled(t *testing.T) {
	e := setupRouter(t)
	admin := loginAdmin(t, e)

	// Exercise the require-2FA gate via a role-scoped flag rather than the
	// global `security.require_2fa` setting: the setting is a single shared
	// row in the test Postgres DB, and `go test ./...` runs packages
	// concurrently as separate processes against that same DB — flipping it
	// here would race with plain admin logins done by other packages'
	// DB-backed tests (billing, subscribers) running at the same time.
	// `twoFactorRequired` ORs the role flag with the global setting, so this
	// covers the same enrolment-grant code path without touching shared state.
	roleName := uniq("forced2fa")
	rr := call(t, e, "POST", "/api/v1/roles", admin.AccessToken, map[string]any{
		"name": roleName, "description": "test", "require_2fa": true, "permissions": []string{},
	})
	if rr.status != http.StatusCreated {
		t.Fatalf("create role = %d: %s", rr.status, rr.body)
	}
	m := createManager(t, e, admin, uniq("forced"), roleName, false)
	pw := "pw-" + m.Username

	// Login now returns an enrolment grant instead of a session.
	r := call(t, e, "POST", "/api/v1/auth/login", "", map[string]string{"username": m.Username, "password": pw})
	if r.status != http.StatusOK {
		t.Fatalf("forced-enrol login = %d: %s", r.status, r.body)
	}
	var body struct {
		Required        bool   `json:"totp_enrollment_required"`
		EnrollmentToken string `json:"enrollment_token"`
	}
	r.json(t, &body)
	if !body.Required || body.EnrollmentToken == "" {
		t.Fatalf("expected enrolment grant: %s", r.body)
	}
	// The enrolment grant authorizes enrol/verify but not a normal endpoint.
	if rr := call(t, e, "GET", "/api/v1/managers", body.EnrollmentToken, nil); rr.status == http.StatusOK {
		t.Fatal("enrolment grant must not authorize normal endpoints")
	}
	er := call(t, e, "POST", "/api/v1/auth/totp/enroll", body.EnrollmentToken, map[string]any{})
	if er.status != http.StatusOK {
		t.Fatalf("enroll with grant = %d: %s", er.status, er.body)
	}
	var enroll enrollResponse
	er.json(t, &enroll)
	code, _ := totpCodeAt(enroll.Secret, time.Now())
	if vr := call(t, e, "POST", "/api/v1/auth/totp/verify", body.EnrollmentToken, map[string]any{"code": code}); vr.status != http.StatusOK {
		t.Fatalf("verify with grant = %d: %s", vr.status, vr.body)
	}
	// Now a full login (with a code) succeeds.
	loginTOTP(t, e, m.Username, pw, enroll.Secret)
}
