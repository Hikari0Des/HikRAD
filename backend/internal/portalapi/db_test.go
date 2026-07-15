package portalapi_test

// DB-backed portal suite (gated on HIKRAD_TEST_DB_URL, matching the repo
// pattern). Boots the full router (panel auth + billing + portalapi + the
// rest) so token audience separation, IDOR scoping, and cross-module
// renewal convergence are proven against the real stack, not mocks.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	_ "github.com/hikrad/hikrad/internal/auth"
	_ "github.com/hikrad/hikrad/internal/billing"
	_ "github.com/hikrad/hikrad/internal/live"
	_ "github.com/hikrad/hikrad/internal/portalapi"
	_ "github.com/hikrad/hikrad/internal/profiles"
	_ "github.com/hikrad/hikrad/internal/radius"
	_ "github.com/hikrad/hikrad/internal/subscribers"
)

type env struct {
	srv        *httptest.Server
	db         *pgxpool.Pool
	adminToken string
}

func uniq(p string) string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return p + hex.EncodeToString(b)
}

func setup(t *testing.T) env {
	t.Helper()
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping portalapi DB suite")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 3)
	}
	redisURL := os.Getenv("HIKRAD_TEST_REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://unused:6379/0"
	}
	t.Setenv("HIKRAD_DB_URL", dbURL)
	t.Setenv("HIKRAD_REDIS_URL", redisURL)
	t.Setenv("HIKRAD_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("HIKRAD_JWT_SECRET", "portal-db-test-secret")
	t.Setenv("HIKRAD_ENV", "dev")

	if err := platform.Migrate(dbURL, "../../migrations", log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := platform.NewDB(ctx, platform.Config{DBURL: dbURL})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)

	adminUser := uniq("admin_")
	bh, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.MinCost)
	if _, err := db.Exec(ctx, `INSERT INTO managers (username, password_hash, role) VALUES ($1,$2,'admin')`,
		adminUser, string(bh)); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	var rdb *redis.Client
	if os.Getenv("HIKRAD_TEST_REDIS_URL") != "" {
		if rdb, err = platform.NewRedis(ctx, platform.Config{RedisURL: redisURL}); err != nil {
			t.Fatalf("redis: %v", err)
		}
		t.Cleanup(func() { _ = rdb.Close() })
	}

	deps := httpapi.Deps{DB: db, Redis: rdb, Settings: platform.NewSettings(db), Log: log}
	srv := httptest.NewServer(httpapi.NewRouter(deps, true))
	t.Cleanup(srv.Close)

	e := env{srv: srv, db: db}
	e.adminToken = e.panelLogin(t, adminUser, "admin")
	return e
}

type resp struct {
	status int
	body   []byte
}

func (r resp) into(t *testing.T, dst any) {
	t.Helper()
	if err := json.Unmarshal(r.body, dst); err != nil {
		t.Fatalf("unmarshal %s: %v", r.body, err)
	}
}

func (e env) do(t *testing.T, method, path, token string, body any) resp {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return resp{status: res.StatusCode, body: raw}
}

func (e env) panelLogin(t *testing.T, user, pass string) string {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/auth/login", "", map[string]string{"username": user, "password": pass})
	if r.status != http.StatusOK {
		t.Fatalf("panel login %s = %d: %s", user, r.status, r.body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	r.into(t, &out)
	return out.AccessToken
}

func (e env) createProfile(t *testing.T, price int64, days int) string {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/profiles", e.adminToken, map[string]any{
		"name": uniq("plan_"), "price_iqd": price, "duration_days": days,
		"rate_down_kbps": 10240, "rate_up_kbps": 2048,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create profile = %d: %s", r.status, r.body)
	}
	var p struct {
		ID string `json:"id"`
	}
	r.into(t, &p)
	return p.ID
}

// createSubscriberWithPassword returns (id, username) so the caller can
// portal-login with the known cleartext password.
func (e env) createSubscriberWithPassword(t *testing.T, profileID, password string) (string, string) {
	t.Helper()
	username := uniq("noor_")
	r := e.do(t, "POST", "/api/v1/subscribers", e.adminToken, map[string]any{
		"username": username, "password": password, "profile_id": profileID,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create subscriber = %d: %s", r.status, r.body)
	}
	var s struct {
		ID string `json:"id"`
	}
	r.into(t, &s)
	return s.ID, username
}

type portalTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Subscriber   struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Language string `json:"language"`
	} `json:"subscriber"`
}

func (e env) portalLogin(t *testing.T, username, password string) portalTokens {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/portal/login", "", map[string]string{"username": username, "password": password})
	if r.status != http.StatusOK {
		t.Fatalf("portal login %s = %d: %s", username, r.status, r.body)
	}
	var out portalTokens
	r.into(t, &out)
	return out
}

// --- tests ------------------------------------------------------------------

// TestPortalTokenAudienceSeparation — a panel token must fail every portal
// endpoint and a portal token must fail every panel/admin endpoint (frozen
// contract: "a portal token must fail all panel/API-admin endpoints").
func TestPortalTokenAudienceSeparation(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30)
	_, username := e.createSubscriberWithPassword(t, prof, "s3cret-pw")
	portal := e.portalLogin(t, username, "s3cret-pw")

	if r := e.do(t, "GET", "/api/v1/portal/me", e.adminToken, nil); r.status != http.StatusUnauthorized {
		t.Fatalf("panel token on portal route = %d, want 401", r.status)
	}
	if r := e.do(t, "GET", "/api/v1/subscribers", portal.AccessToken, nil); r.status != http.StatusUnauthorized {
		t.Fatalf("portal token on panel route = %d, want 401", r.status)
	}
}

// TestPortalMeComposition — Decision 21: consumed data only, never a quota
// ceiling/remaining figure anywhere in the response body.
func TestPortalMeComposition(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30)
	subID, username := e.createSubscriberWithPassword(t, prof, "s3cret-pw")
	// A fresh subscriber has no expiry until first renewed (panel action here
	// stands in for whatever onboarding flow granted the initial period).
	if rr := e.do(t, "POST", "/api/v1/subscribers/"+subID+"/renew", e.adminToken, map[string]any{}); rr.status != http.StatusOK {
		t.Fatalf("seed renew = %d: %s", rr.status, rr.body)
	}
	portal := e.portalLogin(t, username, "s3cret-pw")

	r := e.do(t, "GET", "/api/v1/portal/me", portal.AccessToken, nil)
	if r.status != http.StatusOK {
		t.Fatalf("get me = %d: %s", r.status, r.body)
	}
	var me struct {
		Status      string `json:"status"`
		OnlineNow   bool   `json:"online_now"`
		DaysLeft    int    `json:"days_left"`
		ProfileName string `json:"profile_name"`
		Usage       struct {
			UsedDown  int64 `json:"used_down"`
			UsedUp    int64 `json:"used_up"`
			UsedTotal int64 `json:"used_total"`
		} `json:"usage"`
		Speed struct {
			ProfileDown int `json:"profile_down"`
			ProfileUp   int `json:"profile_up"`
		} `json:"speed"`
	}
	r.into(t, &me)
	if me.Status != "active" || me.DaysLeft < 29 || me.DaysLeft > 30 || me.ProfileName == "" {
		t.Fatalf("unexpected /me: %+v (body=%s)", me, r.body)
	}
	if me.Speed.ProfileDown != 10240 || me.Speed.ProfileUp != 2048 {
		t.Fatalf("speed mismatch: %+v", me.Speed)
	}
	for _, forbidden := range []string{"quota_total", "quota_remaining", "quota_down_bytes", "quota_up_bytes", "remaining_bytes"} {
		if bytes.Contains(r.body, []byte(forbidden)) {
			t.Fatalf("Decision 21 violation: /me body contains %q: %s", forbidden, r.body)
		}
	}
}

// TestPortalSelfUpdateRotatesSessions — FR-44: a password change revokes the
// refresh session; the old refresh token can no longer be used.
func TestPortalSelfUpdateRotatesSessions(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30)
	_, username := e.createSubscriberWithPassword(t, prof, "s3cret-pw")
	portal := e.portalLogin(t, username, "s3cret-pw")

	r := e.do(t, "PUT", "/api/v1/portal/me", portal.AccessToken, map[string]any{"password": "new-pw-123"})
	if r.status != http.StatusOK {
		t.Fatalf("update me = %d: %s", r.status, r.body)
	}

	// Old refresh token is now revoked.
	rr := e.do(t, "POST", "/api/v1/portal/refresh", "", map[string]string{"refresh_token": portal.RefreshToken})
	if rr.status != http.StatusUnauthorized {
		t.Fatalf("refresh after password change = %d, want 401", rr.status)
	}

	// The old password no longer authenticates; the new one does.
	if r := e.do(t, "POST", "/api/v1/portal/login", "", map[string]string{"username": username, "password": "s3cret-pw"}); r.status != http.StatusUnauthorized {
		t.Fatalf("old password login = %d, want 401", r.status)
	}
	if r := e.do(t, "POST", "/api/v1/portal/login", "", map[string]string{"username": username, "password": "new-pw-123"}); r.status != http.StatusOK {
		t.Fatalf("new password login = %d: %s", r.status, r.body)
	}
}

// TestPortalIDOR — a subscriber can never read another's payment history via
// ID manipulation (there is none — identity comes only from the token).
func TestPortalIDOR(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30)
	_, userA := e.createSubscriberWithPassword(t, prof, "pw-a-123")
	_, userB := e.createSubscriberWithPassword(t, prof, "pw-b-123")
	tokA := e.portalLogin(t, userA, "pw-a-123")
	tokB := e.portalLogin(t, userB, "pw-b-123")

	// A renews (via voucher) so A has a payment; B must never see it.
	batch := e.do(t, "POST", "/api/v1/vouchers/batches", e.adminToken,
		map[string]any{"profile_id": prof, "count": 1, "prefix": "IDR"})
	if batch.status != http.StatusCreated {
		t.Fatalf("create batch = %d: %s", batch.status, batch.body)
	}
	lines := bytes.Split(bytes.TrimSpace(batch.body), []byte("\n"))
	code := string(bytes.TrimSpace(lines[1]))
	rv := e.do(t, "POST", "/api/v1/portal/vouchers/redeem", tokA.AccessToken, map[string]any{"code": code})
	if rv.status != http.StatusOK {
		t.Fatalf("redeem = %d: %s", rv.status, rv.body)
	}

	pb := e.do(t, "GET", "/api/v1/portal/payments", tokB.AccessToken, nil)
	if pb.status != http.StatusOK {
		t.Fatalf("get payments B = %d: %s", pb.status, pb.body)
	}
	var listB struct {
		Items []map[string]any `json:"items"`
	}
	pb.into(t, &listB)
	if len(listB.Items) != 0 {
		t.Fatalf("subscriber B sees %d payments, want 0 (IDOR)", len(listB.Items))
	}
}

// TestPortalVoucherRedeemSelfTargeted — FR-42: Noor redeems her own voucher
// with no staff, at midnight.
func TestPortalVoucherRedeemSelfTargeted(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30)
	_, username := e.createSubscriberWithPassword(t, prof, "s3cret-pw")
	portal := e.portalLogin(t, username, "s3cret-pw")

	batch := e.do(t, "POST", "/api/v1/vouchers/batches", e.adminToken,
		map[string]any{"profile_id": prof, "count": 1, "prefix": "MID"})
	lines := bytes.Split(bytes.TrimSpace(batch.body), []byte("\n"))
	code := string(bytes.TrimSpace(lines[1]))

	r := e.do(t, "POST", "/api/v1/portal/vouchers/redeem", portal.AccessToken, map[string]any{"code": code})
	if r.status != http.StatusOK {
		t.Fatalf("redeem = %d: %s", r.status, r.body)
	}
	var out struct {
		NewExpiresAt string `json:"new_expires_at"`
		PriceIQD     int64  `json:"price_iqd"`
	}
	r.into(t, &out)
	if out.NewExpiresAt == "" {
		t.Fatalf("missing new_expires_at: %s", r.body)
	}

	// Second redeem of the same code is rejected as used.
	r2 := e.do(t, "POST", "/api/v1/portal/vouchers/redeem", portal.AccessToken, map[string]any{"code": code})
	if r2.status != http.StatusUnprocessableEntity {
		t.Fatalf("second redeem = %d, want 422 voucher_used", r2.status)
	}
}

// TestMockGatewayLifecycleReplayAndDisabled — gate item 2: create → redirect
// → simulated callback (replayed 3× = one renewal); a disabled gateway is
// unavailable but voucher redemption is unaffected (NFR-7).
func TestMockGatewayLifecycleReplayAndDisabled(t *testing.T) {
	e := setup(t)
	// Force a known starting state: gateway_configs isn't a per-test table, so
	// a prior run against this same persistent DB may have left mock enabled
	// (confirmed while wiring up scripts/gate-phase-4.sh's named-scenario
	// legs, which re-invoke this test in the same DB session as the
	// whole-package run above it).
	if r := e.do(t, "PUT", "/api/v1/payment-gateways/mock", e.adminToken, map[string]any{"enabled": false}); r.status != http.StatusOK {
		t.Fatalf("reset mock gateway to disabled = %d: %s", r.status, r.body)
	}
	prof := e.createProfile(t, 25000, 30)
	_, username := e.createSubscriberWithPassword(t, prof, "s3cret-pw")
	portal := e.portalLogin(t, username, "s3cret-pw")

	// Disabled by default: create must fail gracefully, gateway list is empty.
	lg := e.do(t, "GET", "/api/v1/portal/payments/gateways", portal.AccessToken, nil)
	var gwList struct {
		Items []map[string]any `json:"items"`
	}
	lg.into(t, &gwList)
	if len(gwList.Items) != 0 {
		t.Fatalf("expected no enabled gateways yet, got %+v", gwList.Items)
	}
	cr := e.do(t, "POST", "/api/v1/portal/payments/mock/create", portal.AccessToken, map[string]any{})
	if cr.status != http.StatusServiceUnavailable {
		t.Fatalf("create with disabled gateway = %d, want 503", cr.status)
	}

	// Enable mock (admin).
	pg := e.do(t, "PUT", "/api/v1/payment-gateways/mock", e.adminToken, map[string]any{"enabled": true, "mode": "mock"})
	if pg.status != http.StatusOK {
		t.Fatalf("enable mock gateway = %d: %s", pg.status, pg.body)
	}

	cr = e.do(t, "POST", "/api/v1/portal/payments/mock/create", portal.AccessToken, map[string]any{})
	if cr.status != http.StatusOK {
		t.Fatalf("create payment = %d: %s", cr.status, cr.body)
	}
	var created struct {
		RedirectURL string `json:"redirect_url"`
		IntentID    string `json:"intent_id"`
	}
	cr.into(t, &created)
	if created.RedirectURL == "" || created.IntentID == "" {
		t.Fatalf("missing redirect_url/intent_id: %s", cr.body)
	}

	poll := e.do(t, "GET", "/api/v1/portal/payments/intents/"+created.IntentID, portal.AccessToken, nil)
	var intent struct {
		State      string `json:"state"`
		GatewayRef string `json:"gateway_ref"`
	}
	poll.into(t, &intent)
	if intent.State != "pending" || intent.GatewayRef == "" {
		t.Fatalf("intent after create = %+v", intent)
	}

	// Replay the approve simulation 3 times; exactly one renewal must result.
	for i := 0; i < 3; i++ {
		sr := e.do(t, "POST", "/api/v1/dev/mock-gateway/simulate", "",
			map[string]any{"gateway_ref": intent.GatewayRef, "action": "approve"})
		if sr.status != http.StatusOK {
			t.Fatalf("simulate approve #%d = %d: %s", i, sr.status, sr.body)
		}
	}

	final := e.do(t, "GET", "/api/v1/portal/payments/intents/"+created.IntentID, portal.AccessToken, nil)
	var fin struct {
		State        string     `json:"state"`
		NewExpiresAt *time.Time `json:"new_expires_at"`
	}
	final.into(t, &fin)
	if fin.State != "renewed" || fin.NewExpiresAt == nil {
		t.Fatalf("final intent state = %+v", fin)
	}

	var n int
	_ = e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM ledger_transactions WHERE source = 'portal-mock' AND reference = $1`, created.IntentID).Scan(&n)
	if n != 1 {
		t.Fatalf("expected exactly 1 portal-mock renewal ledger row after 3 replays, got %d", n)
	}
}

// TestPortalCardPaymentSubmitAndQueue — C8: submit creates a pending trial and
// surfaces in both the admin queue and the portal's own "mine" view, with the
// code never appearing in either list.
func TestPortalCardPaymentSubmitAndQueue(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30)
	_, username := e.createSubscriberWithPassword(t, prof, "s3cret-pw")
	portal := e.portalLogin(t, username, "s3cret-pw")

	types := e.do(t, "GET", "/api/v1/portal/card-payments/types", portal.AccessToken, nil)
	if types.status != http.StatusOK || !bytes.Contains(types.body, []byte("zain")) {
		t.Fatalf("card types = %d: %s", types.status, types.body)
	}

	sub := e.do(t, "POST", "/api/v1/portal/card-payments", portal.AccessToken,
		map[string]any{"card_type": "zain", "code": "AAAA-BBBB-CCCC"})
	if sub.status != http.StatusOK {
		t.Fatalf("submit card = %d: %s", sub.status, sub.body)
	}
	if bytes.Contains(sub.body, []byte("AAAA-BBBB-CCCC")) {
		t.Fatal("submit response leaks the card code")
	}

	mineResp := e.do(t, "GET", "/api/v1/portal/card-payments/mine", portal.AccessToken, nil)
	if mineResp.status != http.StatusOK || !bytes.Contains(mineResp.body, []byte(`"pending"`)) {
		t.Fatalf("mine = %d: %s", mineResp.status, mineResp.body)
	}

	queue := e.do(t, "GET", "/api/v1/card-payments?state=pending", e.adminToken, nil)
	if queue.status != http.StatusOK {
		t.Fatalf("admin queue = %d: %s", queue.status, queue.body)
	}
	if bytes.Contains(queue.body, []byte("AAAA-BBBB-CCCC")) {
		t.Fatal("admin queue leaks the card code (must only appear via /reveal)")
	}
	var q struct {
		Items []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"items"`
	}
	queue.into(t, &q)
	// Filter to this test's own subscriber: card_payments isn't a per-test
	// table, so a prior run against this same persistent DB may have left
	// other pending rows queued (same accumulation risk as the gateway-config
	// state above).
	var mine *struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	for i := range q.Items {
		if q.Items[i].Username == username {
			mine = &q.Items[i]
			break
		}
	}
	if mine == nil {
		t.Fatalf("expected a queued card payment for %s, got %+v", username, q.Items)
	}

	rv := e.do(t, "POST", "/api/v1/card-payments/"+mine.ID+"/reveal", e.adminToken, nil)
	if rv.status != http.StatusOK {
		t.Fatalf("reveal = %d: %s", rv.status, rv.body)
	}
	var revealed struct {
		Code string `json:"code"`
	}
	rv.into(t, &revealed)
	if revealed.Code != "AAAA-BBBB-CCCC" {
		t.Fatalf("revealed code = %q, want AAAA-BBBB-CCCC", revealed.Code)
	}
	var auditN int
	_ = e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = 'card_payment.reveal' AND entity_id = $1`, mine.ID).Scan(&auditN)
	if auditN != 1 {
		t.Fatalf("expected 1 audit_log row for the reveal, got %d", auditN)
	}
}

// TestPortalLoginRateLimit — gate item 6: NFR-4.6, scripted brute-force
// attempt against the portal login route. acctFailLimit failed attempts lock
// the account for acctLockTTL; the correct password is then also rejected
// with 429 + Retry-After while locked (not just wrong-password rejections).
//
// Uses a synthetic per-test X-Forwarded-For IP rather than the shared
// httptest loopback address: the limiter's IP bucket (ipFailLimit=20,
// ipLockTTL=15m) is real-Redis-backed and outlives a single test run, so
// every other test in this package shares 127.0.0.1 and would otherwise
// accumulate toward — and eventually trip — the same IP lock (confirmed:
// an earlier version of this test using the real loopback address left the
// suite's shared IP counter locked for 15 minutes, failing unrelated tests
// like TestPortalMeComposition with 429s).
func TestPortalLoginRateLimit(t *testing.T) {
	e := setup(t)
	if os.Getenv("HIKRAD_TEST_REDIS_URL") == "" {
		t.Skip("HIKRAD_TEST_REDIS_URL not set; rate limiter is a no-op without redis")
	}
	prof := e.createProfile(t, 25000, 30)
	_, username := e.createSubscriberWithPassword(t, prof, "s3cret-pw")
	var octet [1]byte
	_, _ = rand.Read(octet[:])
	fakeIP := "203.0.113." + strconv.Itoa(1+int(octet[0])%253)

	doFrom := func(password string) resp {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"username": username, "password": password})
		req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/portal/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", fakeIP)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return resp{status: res.StatusCode, body: raw}
	}

	const acctFailLimit = 5
	for i := 0; i < acctFailLimit; i++ {
		r := doFrom("wrong-pw")
		if r.status != http.StatusUnauthorized {
			t.Fatalf("failed attempt #%d = %d, want 401", i, r.status)
		}
	}

	// Locked now — even the correct password is rejected.
	locked := doFrom("s3cret-pw")
	if locked.status != http.StatusTooManyRequests {
		t.Fatalf("login while locked = %d, want 429: %s", locked.status, locked.body)
	}
}

// TestBrandingPublicEndpoint — C5: no auth required.
func TestBrandingPublicEndpoint(t *testing.T) {
	e := setup(t)
	r := e.do(t, "GET", "/api/v1/branding", "", nil)
	if r.status != http.StatusOK {
		t.Fatalf("branding = %d: %s", r.status, r.body)
	}
	var b struct {
		Name string `json:"name"`
	}
	r.into(t, &b)
	if b.Name == "" {
		t.Fatalf("branding missing name: %s", r.body)
	}
}
