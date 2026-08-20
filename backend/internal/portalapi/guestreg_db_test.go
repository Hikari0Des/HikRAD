package portalapi_test

// FR-169 (self-service guest registration) + FR-172 (OTP login) suite,
// gated on HIKRAD_TEST_DB_URL + HIKRAD_TEST_REDIS_URL (notify's dispatcher
// isn't run here — these tests assert the OUTBOX row, not delivery, same
// posture v4-10's own suite uses for the identical reason).

import (
	"context"
	"crypto/rand"
	"net/http"
	"strconv"
	"testing"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/platform/license"
	"github.com/hikrad/hikrad/internal/subscribers"
)

// uniqDigits returns 9 random digits (a plausible local-number tail) so
// tests using a phone number as the row/username key never collide with a
// previous run against the same shared database.
func uniqDigits(t *testing.T) string {
	t.Helper()
	var out string
	for len(out) < 9 {
		b := make([]byte, 1)
		_, _ = rand.Read(b)
		out += strconv.Itoa(int(b[0]) % 10)
	}
	return out
}

// The settings row this feature reads is a DB-wide singleton (see
// settings_api.go), and this suite's tests share ONE database across a
// single `go test` run (db_test.go's setup() migrates/connects to the same
// HIKRAD_TEST_DB_URL every time, no per-test database) — so every test that
// depends on the enabled/disabled state must establish it explicitly rather
// than relying on "this is a fresh install," which is only true for
// whichever test happens to run first.
func (e env) setGuestRegistrationEnabled(t *testing.T, enabled bool, profileID string) {
	t.Helper()
	r := e.do(t, "PUT", "/api/v1/settings/guest_registration", e.adminToken, map[string]any{
		"enabled": enabled, "daily_cap": 100, "passcode_ttl_minutes": 10, "cooldown_minutes": 5,
		"default_profile_id": profileID,
	})
	if r.status != http.StatusOK {
		t.Fatalf("set guest registration enabled=%v = %d: %s", enabled, r.status, r.body)
	}
}

// The self_service entitlement is a real license gate (FR-169 §3.4: "daily
// volume capped by licence entitlement") — a fresh/unlicensed install grants
// NO entitlements (platform.LicenseHasEntitlement's own doc comment: "false
// ... when no license is installed"), so the positive-path tests must grant
// it explicitly, the same way internal/httpapi/license_gate_test.go does.
func grantSelfServiceEntitlement(t *testing.T) {
	t.Helper()
	platform.SetCachedLicenseSnapshotForTest(platform.LicenseSnapshot{
		State:        license.StateValid,
		Entitlements: map[string]bool{license.EntitlementSelfService: true},
	})
	t.Cleanup(func() {
		platform.SetCachedLicenseSnapshotForTest(platform.LicenseSnapshot{State: license.StateValid})
	})
}

// latestPasscode reads the just-issued plaintext-adjacent state the test
// needs to "receive" the code exactly as a real SMS/WhatsApp channel would
// hand it to the visitor — by reading the outbox row's rendered body
// (notify.EnqueuePasscode renders {{code}} into it) rather than the DB's
// own sha256(code) column, which is deliberately one-way.
func (e env) latestPasscodeCodeFor(t *testing.T, event, address string) string {
	t.Helper()
	var body string
	err := e.db.QueryRow(context.Background(),
		`SELECT body FROM notification_outbox WHERE event = $1 AND address = $2 ORDER BY created_at DESC LIMIT 1`,
		event, address).Scan(&body)
	if err != nil {
		t.Fatalf("read passcode outbox row: %v", err)
	}
	// body = "Your HikRAD verification code is 123456. It expires in 10 minutes."
	for _, tok := range splitWords(body) {
		if len(tok) == 6 && isDigits(tok) {
			return tok
		}
	}
	t.Fatalf("no 6-digit code found in outbox body: %q", body)
	return ""
}

func splitWords(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' || r == '.' || r == ',' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func TestGuestRegistration_EndToEnd(t *testing.T) {
	e := autologinEnv(t) // reuses the DB+Redis skip gate
	grantSelfServiceEntitlement(t)
	prof := e.createProfile(t, 15000, 30)
	e.setGuestRegistrationEnabled(t, true, prof)

	// A unique phone per run, not a fixed literal: guest_passcode_attempts/
	// notification_outbox are shared, DB-wide tables with no per-test
	// isolation, so a hardcoded number would collide across repeated suite
	// runs against the same database exactly like a hardcoded username would.
	phone := "+9647" + uniqDigits(t)
	r := e.do(t, "POST", "/api/v1/portal/register", "", map[string]any{"name": "Noor Ahmed", "phone": phone, "mac": "AA:BB:CC:DD:EE:FF"})
	if r.status != http.StatusOK {
		t.Fatalf("register = %d: %s", r.status, r.body)
	}

	// A re-ask while the code is still live must NOT send a second one —
	// same outbox row count, "already_sent" in the response.
	r2 := e.do(t, "POST", "/api/v1/portal/register", "", map[string]any{"name": "Noor Ahmed", "phone": phone})
	if r2.status != http.StatusOK {
		t.Fatalf("re-ask register = %d: %s", r2.status, r2.body)
	}
	var re struct {
		Sent        bool `json:"sent"`
		AlreadySent bool `json:"already_sent"`
	}
	r2.into(t, &re)
	if !re.AlreadySent {
		t.Fatalf("re-ask response = %+v, want already_sent=true", re)
	}
	var outboxCount int
	_ = e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM notification_outbox WHERE event = 'guest_registration_passcode' AND address = $1`, phone).Scan(&outboxCount)
	if outboxCount != 1 {
		t.Fatalf("outbox rows for %s = %d, want exactly 1 (no second send on re-ask)", phone, outboxCount)
	}

	code := e.latestPasscodeCodeFor(t, "guest_registration_passcode", phone)

	// Wrong code refused.
	bad := e.do(t, "POST", "/api/v1/portal/register/verify", "", map[string]any{"phone": phone, "code": "000000"})
	if bad.status != http.StatusUnauthorized {
		t.Fatalf("verify wrong code = %d, want 401: %s", bad.status, bad.body)
	}

	// Right code creates the account and logs in.
	ok := e.do(t, "POST", "/api/v1/portal/register/verify", "", map[string]any{"phone": phone, "code": code})
	if ok.status != http.StatusOK {
		t.Fatalf("verify correct code = %d: %s", ok.status, ok.body)
	}
	var tok portalTokens
	ok.into(t, &tok)
	if tok.Subscriber.Username != phone {
		t.Fatalf("created account username = %q, want the phone number %q", tok.Subscriber.Username, phone)
	}
	if tok.Scope != "" && tok.Scope != "full" {
		t.Fatalf("registration login scope = %q, want full", tok.Scope)
	}

	// The SAME code cannot be replayed (consumed).
	replay := e.do(t, "POST", "/api/v1/portal/register/verify", "", map[string]any{"phone": phone, "code": code})
	if replay.status != http.StatusUnauthorized {
		t.Fatalf("replayed code = %d, want 401", replay.status)
	}
}

func TestGuestRegistration_CooldownAfterExpiry(t *testing.T) {
	e := autologinEnv(t)
	grantSelfServiceEntitlement(t)
	prof := e.createProfile(t, 15000, 30)
	e.setGuestRegistrationEnabled(t, true, prof)

	phone := "+9647" + uniqDigits(t)
	r := e.do(t, "POST", "/api/v1/portal/register", "", map[string]any{"name": "X", "phone": phone})
	if r.status != http.StatusOK {
		t.Fatalf("register = %d: %s", r.status, r.body)
	}
	// Force the just-issued code into the past (simulating it going unused
	// past its TTL) without a real wait — cooldown_minutes defaults to 5,
	// far longer than "just expired".
	if _, err := e.db.Exec(context.Background(),
		`UPDATE guest_passcodes SET expires_at = now() - interval '10 seconds'
		  WHERE purpose = 'register' AND phone = $1 AND consumed_at IS NULL`, phone); err != nil {
		t.Fatal(err)
	}

	r2 := e.do(t, "POST", "/api/v1/portal/register", "", map[string]any{"name": "X", "phone": phone})
	if r2.status != http.StatusTooManyRequests {
		t.Fatalf("re-request during cooldown = %d, want 429: %s", r2.status, r2.body)
	}

	// Push the expiry further back — past the cooldown window — and confirm
	// a fresh request now succeeds (the superseded row must not permanently
	// block re-registration via the guest_passcodes_live_idx unique index).
	if _, err := e.db.Exec(context.Background(),
		`UPDATE guest_passcodes SET expires_at = now() - interval '10 minutes'
		  WHERE purpose = 'register' AND phone = $1 AND consumed_at IS NULL`, phone); err != nil {
		t.Fatal(err)
	}
	r3 := e.do(t, "POST", "/api/v1/portal/register", "", map[string]any{"name": "X", "phone": phone})
	if r3.status != http.StatusOK {
		t.Fatalf("re-request after cooldown elapsed = %d, want 200: %s", r3.status, r3.body)
	}
}

func TestGuestRegistration_DailyCapReached(t *testing.T) {
	e := autologinEnv(t)
	grantSelfServiceEntitlement(t)
	prof := e.createProfile(t, 15000, 30)

	// The cap is a real GLOBAL 24h counter (by design — one licence
	// entitlement caps the whole surface, not per-test), so this suite's
	// own earlier sends within the same shared database count toward it.
	// Read the current count and set the cap to exactly "one more than
	// what's already gone out today" so this test's own first send is
	// guaranteed to still land inside the cap regardless of run order.
	var sentSoFar int
	if err := e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM guest_passcode_attempts WHERE action = 'send' AND at >= now() - interval '24 hours'`).
		Scan(&sentSoFar); err != nil {
		t.Fatal(err)
	}
	r := e.do(t, "PUT", "/api/v1/settings/guest_registration", e.adminToken, map[string]any{
		"enabled": true, "daily_cap": sentSoFar + 1, "passcode_ttl_minutes": 10, "cooldown_minutes": 5,
		"default_profile_id": prof,
	})
	if r.status != http.StatusOK {
		t.Fatalf("set daily_cap = %d: %s", r.status, r.body)
	}

	first := e.do(t, "POST", "/api/v1/portal/register", "", map[string]any{"name": "A", "phone": "+9647" + uniqDigits(t)})
	if first.status != http.StatusOK {
		t.Fatalf("first register (within cap) = %d: %s", first.status, first.body)
	}
	second := e.do(t, "POST", "/api/v1/portal/register", "", map[string]any{"name": "B", "phone": "+9647" + uniqDigits(t)})
	if second.status != http.StatusTooManyRequests {
		t.Fatalf("second register (over the cap) = %d, want 429: %s", second.status, second.body)
	}
}

func TestGuestRegistration_OffByDefault(t *testing.T) {
	e := autologinEnv(t)
	grantSelfServiceEntitlement(t) // licensed but still OFF — enabled is its own, separate gate
	e.setGuestRegistrationEnabled(t, false, "")
	r := e.do(t, "POST", "/api/v1/portal/register", "", map[string]any{"name": "X", "phone": "+9647709999999"})
	if r.status != http.StatusNotFound {
		t.Fatalf("register while disabled = %d, want 404 (off by default): %s", r.status, r.body)
	}
}

func TestOTPLogin_PasswordlessAccount(t *testing.T) {
	e := autologinEnv(t)
	grantSelfServiceEntitlement(t)
	prof := e.createProfile(t, 15000, 30)
	e.setGuestRegistrationEnabled(t, true, prof)

	// Seed a passwordless account directly (the closed-known-issues case:
	// v1.1 no_password, or FR-169's own output — either way, has_password=false).
	username := "+9647" + uniqDigits(t)
	subID, err := subscribers.CreateGuestAccount(context.Background(), e.db, username, "Hassan", prof)
	if err != nil {
		t.Fatal(err)
	}

	// A normal password login must still be refused for this account (no
	// credential path exists other than OTP).
	pw := e.do(t, "POST", "/api/v1/portal/login", "", map[string]string{"username": username, "password": "anything"})
	if pw.status != http.StatusUnauthorized {
		t.Fatalf("password login on a passwordless account = %d, want 401", pw.status)
	}

	req := e.do(t, "POST", "/api/v1/portal/otp/request", "", map[string]any{"username": username})
	if req.status != http.StatusOK {
		t.Fatalf("otp request = %d: %s", req.status, req.body)
	}
	code := e.latestPasscodeCodeFor(t, "otp_login_passcode", username)

	verify := e.do(t, "POST", "/api/v1/portal/otp/verify", "", map[string]any{"username": username, "code": code})
	if verify.status != http.StatusOK {
		t.Fatalf("otp verify = %d: %s", verify.status, verify.body)
	}
	var tok portalTokens
	verify.into(t, &tok)
	if tok.Subscriber.ID != subID {
		t.Fatalf("otp login subscriber id = %q, want %q", tok.Subscriber.ID, subID)
	}
}

func TestOTPLogin_RefusesAccountsWithAPassword(t *testing.T) {
	e := autologinEnv(t)
	prof := e.createProfile(t, 15000, 30)
	_, username := e.createSubscriberWithPassword(t, prof, "s3cret-pw")

	r := e.do(t, "POST", "/api/v1/portal/otp/request", "", map[string]any{"username": username})
	if r.status != http.StatusUnauthorized {
		t.Fatalf("otp request for a password-having account = %d, want 401: %s", r.status, r.body)
	}
}
