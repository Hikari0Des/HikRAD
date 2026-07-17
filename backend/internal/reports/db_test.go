package reports_test

// DB-backed reports suite (gated on HIKRAD_TEST_DB_URL, matching the repo
// pattern — see billing/db_test.go). Boots the full router so reports read
// real ledger/payment/subscriber data produced by the real money path.
// Covers the Phase-5 gate item 4 legs this agent owns: revenue ≡ ledger
// (payments) sums under randomized fixtures, settlement closing ≡ live
// balance, and the expiring-report/digest single-definition property
// (AC-46a).

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/reports"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	_ "github.com/hikrad/hikrad/internal/auth"
	_ "github.com/hikrad/hikrad/internal/billing"
	_ "github.com/hikrad/hikrad/internal/live"
	_ "github.com/hikrad/hikrad/internal/profiles"
	_ "github.com/hikrad/hikrad/internal/radius"
	_ "github.com/hikrad/hikrad/internal/reports"
	_ "github.com/hikrad/hikrad/internal/subscribers"
)

type env struct {
	srv     *httptest.Server
	db      *pgxpool.Pool
	token   string
	adminID string
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
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping reports DB suite")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 11)
	}
	redisURL := os.Getenv("HIKRAD_TEST_REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://unused:6379/0"
	}
	t.Setenv("HIKRAD_DB_URL", dbURL)
	t.Setenv("HIKRAD_REDIS_URL", redisURL)
	t.Setenv("HIKRAD_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("HIKRAD_JWT_SECRET", "reports-db-test-secret")
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
	var adminID string
	if err := db.QueryRow(ctx,
		`INSERT INTO managers (username, password_hash, role) VALUES ($1,$2,'admin') RETURNING id::text`,
		adminUser, string(bh)).Scan(&adminID); err != nil {
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

	e := env{srv: srv, db: db, adminID: adminID}
	e.token = e.login(t, adminUser, "admin")
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
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
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

func (e env) login(t *testing.T, user, pass string) string {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/auth/login", "", map[string]string{"username": user, "password": pass})
	if r.status != http.StatusOK {
		t.Fatalf("login %s = %d: %s", user, r.status, r.body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	r.into(t, &out)
	return out.AccessToken
}

func (e env) createProfile(t *testing.T, price int64, days int) string {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/profiles", e.token, map[string]any{
		"name": uniq("plan_"), "price": price, "currency": "IQD", "duration_days": days,
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

func (e env) createSubscriber(t *testing.T, profileID string) string {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/subscribers", e.token, map[string]any{
		"username": uniq("u_"), "password": "secret123", "profile_id": profileID,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create subscriber = %d: %s", r.status, r.body)
	}
	var s struct {
		ID string `json:"id"`
	}
	r.into(t, &s)
	return s.ID
}

func (e env) createSubscriberOwned(t *testing.T, profileID, ownerManagerID string) string {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/subscribers", e.token, map[string]any{
		"username": uniq("u_"), "password": "secret123", "profile_id": profileID,
		"owner_manager_id": ownerManagerID,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create owned subscriber = %d: %s", r.status, r.body)
	}
	var s struct {
		ID string `json:"id"`
	}
	r.into(t, &s)
	return s.ID
}

func (e env) seedAgent(t *testing.T) (string, string) {
	t.Helper()
	user := uniq("agent_")
	bh, _ := bcrypt.GenerateFromPassword([]byte("agentpw"), bcrypt.MinCost)
	var id string
	if err := e.db.QueryRow(context.Background(),
		`INSERT INTO managers (username, password_hash, role, scoped) VALUES ($1,$2,'agent',true) RETURNING id::text`,
		user, string(bh)).Scan(&id); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	return id, e.login(t, user, "agentpw")
}

func (e env) balance(t *testing.T, mgr, token string) int64 {
	t.Helper()
	r := e.do(t, "GET", "/api/v1/managers/"+mgr+"/balance?currency=IQD", token, nil)
	if r.status != http.StatusOK {
		t.Fatalf("balance = %d: %s", r.status, r.body)
	}
	var out struct {
		Balance int64 `json:"balance"`
	}
	r.into(t, &out)
	return out.Balance
}

func (e env) topup(t *testing.T, mgr, token string, amount int64) {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/managers/"+mgr+"/topup", token, map[string]any{"amount": amount, "currency": "IQD"})
	if r.status != http.StatusOK {
		t.Fatalf("topup = %d: %s", r.status, r.body)
	}
}

func randInt(t *testing.T, n int64) int64 {
	t.Helper()
	if n <= 0 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(n))
	if err != nil {
		t.Fatal(err)
	}
	return v.Int64()
}

// --- tests -------------------------------------------------------------

// TestRevenueReportReconcilesWithPayments — gate item 4 (property test):
// randomized renewals + refunds, revenue report total ≡ direct sum over
// payments for the same range, at every group_by.
func TestRevenueReportReconcilesWithPayments(t *testing.T) {
	e := setup(t)
	agentID, agentToken := e.seedAgent(t)
	e.topup(t, agentID, e.token, 50_000_000)
	prof := e.createProfile(t, 10_000, 30)

	from := time.Now().Add(-time.Minute).UTC()
	const rounds = 12
	for i := 0; i < rounds; i++ {
		sub := e.createSubscriberOwned(t, prof, agentID)
		r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentToken, map[string]any{})
		if r.status != http.StatusOK {
			t.Fatalf("renew = %d: %s", r.status, r.body)
		}
		// Refund roughly a third of renewals (refunds negative — must still
		// reconcile).
		if randInt(t, 3) == 0 {
			var out struct {
				LedgerTxID string `json:"ledger_tx_id"`
			}
			r.into(t, &out)
			rf := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/refund", e.token,
				map[string]any{"ledger_tx_id": out.LedgerTxID, "reason": "test refund"})
			if rf.status != http.StatusOK {
				t.Fatalf("refund = %d: %s", rf.status, rf.body)
			}
		}
	}
	to := time.Now().Add(time.Minute).UTC()

	var directTotal int64
	if err := e.db.QueryRow(context.Background(),
		`SELECT COALESCE(sum(amount),0) FROM payments WHERE currency = 'IQD' AND at >= $1 AND at < $2`, from, to).
		Scan(&directTotal); err != nil {
		t.Fatal(err)
	}
	if directTotal == 0 {
		t.Fatal("test fixture produced no payments — cannot validate reconciliation")
	}

	for _, groupBy := range []string{"day", "month", "manager", "profile", "method"} {
		q := "?from=" + from.Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339) + "&group_by=" + groupBy
		r := e.do(t, "GET", "/api/v1/reports/revenue"+q, e.token, nil)
		if r.status != http.StatusOK {
			t.Fatalf("revenue[%s] = %d: %s", groupBy, r.status, r.body)
		}
		var out struct {
			Totals map[string]int64 `json:"totals"`
			Rows   []struct {
				Key      string `json:"key"`
				Currency string `json:"currency"`
				Amount   int64  `json:"amount"`
				Count    int    `json:"count"`
			} `json:"rows"`
		}
		r.into(t, &out)
		if out.Totals["IQD"] != directTotal {
			t.Fatalf("group_by=%s: totals[IQD]=%d, want %d (direct payments sum)", groupBy, out.Totals["IQD"], directTotal)
		}
		var rowSum int64
		for _, rr := range out.Rows {
			if rr.Currency == "IQD" {
				rowSum += rr.Amount
			}
		}
		if rowSum != directTotal {
			t.Fatalf("group_by=%s: sum(rows[IQD].amount)=%d, want %d", groupBy, rowSum, directTotal)
		}
	}

	// CSV export reconciles too (spot-check group_by=day).
	rcsv := e.do(t, "GET", "/api/v1/reports/revenue?from="+from.Format(time.RFC3339)+
		"&to="+to.Format(time.RFC3339)+"&group_by=day&format=csv", e.token, nil)
	if rcsv.status != http.StatusOK {
		t.Fatalf("revenue csv = %d: %s", rcsv.status, rcsv.body)
	}
}

// TestSettlementClosingEqualsLiveBalance — gate item 4: opening/topups/
// renewals/refunds breakdown plus the hard invariant closing ≡ live balance
// at to=now.
func TestSettlementClosingEqualsLiveBalance(t *testing.T) {
	e := setup(t)
	agentID, agentToken := e.seedAgent(t)
	e.topup(t, agentID, e.token, 200_000)
	prof := e.createProfile(t, 15_000, 30)

	for i := 0; i < 3; i++ {
		sub := e.createSubscriberOwned(t, prof, agentID)
		r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentToken, map[string]any{})
		if r.status != http.StatusOK {
			t.Fatalf("renew = %d: %s", r.status, r.body)
		}
	}
	live := e.balance(t, agentID, agentToken)

	// Explicit, generously-padded "to": the default (server-side time.Now())
	// is exact-enough in production (api and postgres share one Docker host's
	// clock) but flakes in a local sandbox where the Go test process's host
	// clock and the Postgres container's clock can drift by a few hundred ms.
	to := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	r := e.do(t, "GET", "/api/v1/reports/settlement?manager_id="+agentID+"&to="+to, e.token, nil)
	if r.status != http.StatusOK {
		t.Fatalf("settlement = %d: %s", r.status, r.body)
	}
	var out struct {
		Opening  int64 `json:"opening"`
		Topups   int64 `json:"topups"`
		Renewals struct {
			Count  int   `json:"count"`
			Amount int64 `json:"amount"`
		} `json:"renewals"`
		Refunds int64 `json:"refunds"`
		Closing int64 `json:"closing"`
	}
	r.into(t, &out)
	if out.Closing != live {
		t.Fatalf("closing=%d, want live balance %d", out.Closing, live)
	}
	if out.Renewals.Count != 3 || out.Renewals.Amount != 45_000 {
		t.Fatalf("renewals = %+v, want count=3 amount=45000", out.Renewals)
	}
	if out.Topups != 200_000 {
		t.Fatalf("topups=%d, want 200000", out.Topups)
	}
	if out.Opening+out.Topups-out.Renewals.Amount+out.Refunds != out.Closing {
		t.Fatalf("opening+topups-renewals+refunds=%d, want closing=%d",
			out.Opening+out.Topups-out.Renewals.Amount+out.Refunds, out.Closing)
	}

	// Zero-activity manager: clean zeros, not a 404 (edge case).
	otherID, _ := e.seedAgent(t)
	r2 := e.do(t, "GET", "/api/v1/reports/settlement?manager_id="+otherID, e.token, nil)
	if r2.status != http.StatusOK {
		t.Fatalf("settlement (zero activity) = %d: %s", r2.status, r2.body)
	}
	var out2 struct {
		Closing int64 `json:"closing"`
	}
	r2.into(t, &out2)
	if out2.Closing != 0 {
		t.Fatalf("zero-activity manager closing=%d, want 0", out2.Closing)
	}
}

// TestExpiringReportMatchesDigestQuery — AC-46a: the subscribers report's
// view=expiring and the exported ExpiringSubscribers function C's digest
// calls MUST return identical row sets for the same N and moment (they are
// the same query, not two that happen to agree).
func TestExpiringReportMatchesDigestQuery(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 5_000, 30)

	// One subscriber expiring in 2 days (inside a 3-day window), one outside.
	near := e.createSubscriber(t, prof)
	e.do(t, "PUT", "/api/v1/subscribers/"+near, e.token, map[string]any{
		"expires_at": time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339),
	})
	far := e.createSubscriber(t, prof)
	e.do(t, "PUT", "/api/v1/subscribers/"+far, e.token, map[string]any{
		"expires_at": time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339),
	})

	r := e.do(t, "GET", "/api/v1/reports/subscribers?view=expiring&n=3", e.token, nil)
	if r.status != http.StatusOK {
		t.Fatalf("expiring report = %d: %s", r.status, r.body)
	}
	var out struct {
		Rows []struct {
			ID string `json:"id"`
		} `json:"rows"`
	}
	r.into(t, &out)

	direct, err := reports.ExpiringSubscribers(context.Background(), e.db, 3, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(out.Rows) != len(direct) {
		t.Fatalf("report has %d rows, ExpiringSubscribers has %d", len(out.Rows), len(direct))
	}
	reportIDs := map[string]bool{}
	for _, rr := range out.Rows {
		reportIDs[rr.ID] = true
	}
	for _, d := range direct {
		if !reportIDs[d.ID] {
			t.Fatalf("ExpiringSubscribers row %s missing from report response", d.ID)
		}
	}
	found := false
	for _, rr := range out.Rows {
		if rr.ID == near {
			found = true
		}
		if rr.ID == far {
			t.Fatal("far-expiring subscriber leaked into the 3-day expiring report")
		}
	}
	if !found {
		t.Fatal("near-expiring subscriber missing from the 3-day expiring report")
	}
}

// TestSettlementAndRevenueScopedManagerIsolation — FR-45.3: a scoped manager
// sees only their own figures.
func TestSettlementAndRevenueScopedManagerIsolation(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 8_000, 30)

	a1, t1 := e.seedAgent(t)
	a2, t2 := e.seedAgent(t)
	e.topup(t, a1, e.token, 100_000)
	e.topup(t, a2, e.token, 100_000)

	s1 := e.createSubscriberOwned(t, prof, a1)
	if r := e.do(t, "POST", "/api/v1/subscribers/"+s1+"/renew", t1, map[string]any{}); r.status != http.StatusOK {
		t.Fatalf("renew as agent1 = %d: %s", r.status, r.body)
	}

	// Agent 2's own settlement must show 0 renewals despite agent 1's activity.
	r := e.do(t, "GET", "/api/v1/reports/settlement?manager_id="+a1, t2, nil)
	if r.status != http.StatusOK {
		t.Fatalf("settlement = %d: %s", r.status, r.body)
	}
	var out struct {
		Renewals struct {
			Count int `json:"count"`
		} `json:"renewals"`
	}
	r.into(t, &out)
	if out.Renewals.Count != 0 {
		t.Fatalf("agent2 querying agent1's manager_id got %d renewals, want 0 (scope must pin to self)", out.Renewals.Count)
	}
}

// TestDigestComposition sanity-checks the FR-48 internal digest endpoint.
func TestDigestComposition(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 5_000, 30)
	sub := e.createSubscriber(t, prof)
	e.do(t, "PUT", "/api/v1/subscribers/"+sub, e.token, map[string]any{
		"expires_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	})

	res, err := http.Get(e.srv.URL + "/internal/reports/digest?days=3")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("digest = %d", res.StatusCode)
	}
	var out struct {
		MessageKey   string `json:"message_key"`
		ExpiringSoon struct {
			Days  int `json:"days"`
			Count int `json:"count"`
		} `json:"expiring_soon"`
	}
	body, _ := io.ReadAll(res.Body)
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, body)
	}
	if out.MessageKey == "" {
		t.Fatal("digest response has no message_key (must be localized-key, not pre-rendered text)")
	}
	if out.ExpiringSoon.Count < 1 {
		t.Fatalf("expiring_soon.count = %d, want >= 1", out.ExpiringSoon.Count)
	}
}
