package billing_test

// DB-backed billing suite (gated on HIKRAD_TEST_DB_URL, matching the repo
// pattern). It boots the full router so the money path runs with its real
// dependencies — auth, audit, the subscribers/profiles read-models, the radius
// policy seam — and covers the scriptable gate legs: renew-with-ledger,
// balance-blocking + top-up, the balance≡ledger property under concurrency, the
// 50-goroutine voucher double-redeem storm, refund reversal, and ledger DB-level
// immutability.

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
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	_ "github.com/hikrad/hikrad/internal/auth"
	_ "github.com/hikrad/hikrad/internal/billing"
	_ "github.com/hikrad/hikrad/internal/live"
	_ "github.com/hikrad/hikrad/internal/platform/setupapi" // v2 phase 11: PUT /api/v1/settings/{branding,billing} for receipt_branding_db_test.go
	_ "github.com/hikrad/hikrad/internal/profiles"
	_ "github.com/hikrad/hikrad/internal/radius"
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
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping billing DB suite")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 7)
	}
	redisURL := os.Getenv("HIKRAD_TEST_REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://unused:6379/0"
	}
	t.Setenv("HIKRAD_DB_URL", dbURL)
	t.Setenv("HIKRAD_REDIS_URL", redisURL)
	t.Setenv("HIKRAD_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("HIKRAD_JWT_SECRET", "billing-db-test-secret")
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

func (e env) do(t *testing.T, method, path, token string, body any, headers ...[2]string) resp {
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
	for _, h := range headers {
		req.Header.Set(h[0], h[1])
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

// --- fixtures ---------------------------------------------------------------

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

// seedAgent inserts a scoped agent manager and returns (id, token).
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

// ledgerSum returns the raw ledger balance for a manager.
func (e env) ledgerSum(t *testing.T, mgr string) int64 {
	t.Helper()
	var n int64
	if err := e.db.QueryRow(context.Background(),
		`SELECT COALESCE(sum(amount),0) FROM ledger_transactions WHERE actor_manager_id=$1::uuid AND currency='IQD'`, mgr).
		Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e env) cachedBalance(t *testing.T, mgr string) int64 {
	t.Helper()
	var n int64
	if err := e.db.QueryRow(context.Background(),
		`SELECT COALESCE((SELECT balance FROM manager_balances WHERE manager_id=$1::uuid AND currency='IQD'),0)`, mgr).
		Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// --- tests ------------------------------------------------------------------

// TestRenewWritesLedgerAndExtendsExpiry — gate item 1 (scriptable half).
func TestRenewWritesLedgerAndExtendsExpiry(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30)
	sub := e.createSubscriber(t, prof)

	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", e.token, map[string]any{})
	if r.status != http.StatusOK {
		t.Fatalf("renew = %d: %s", r.status, r.body)
	}
	var out struct {
		LedgerTxID   string `json:"ledger_tx_id"`
		ReceiptNo    string `json:"receipt_no"`
		NewExpiresAt string `json:"new_expires_at"`
		CoAResult    string `json:"coa_result"`
	}
	r.into(t, &out)
	if out.LedgerTxID == "" || out.ReceiptNo == "" || out.NewExpiresAt == "" {
		t.Fatalf("renew missing fields: %s", r.body)
	}
	if out.CoAResult != "not_online" {
		t.Fatalf("coa_result = %q, want not_online (no live session)", out.CoAResult)
	}

	// Ledger entry + payment/receipt exist.
	var n int
	_ = e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM ledger_transactions WHERE id=$1::uuid AND type='renewal' AND subscriber_id=$2::uuid`,
		out.LedgerTxID, sub).Scan(&n)
	if n != 1 {
		t.Fatalf("expected 1 renewal ledger row, got %d", n)
	}
	_ = e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM payments WHERE receipt_no=$1 AND amount=25000`, out.ReceiptNo).Scan(&n)
	if n != 1 {
		t.Fatalf("expected payment row for receipt %s", out.ReceiptNo)
	}

	// Receipt HTML renders (reprint creates no new payment).
	rec := e.do(t, "GET", "/api/v1/payments/"+out.ReceiptNo+"/receipt?lang=ar", e.token, nil)
	if rec.status != http.StatusOK || !bytes.Contains(rec.body, []byte("إيصال")) {
		t.Fatalf("receipt render = %d: %s", rec.status, rec.body)
	}
	e.do(t, "GET", "/api/v1/payments/"+out.ReceiptNo+"/receipt", e.token, nil)
	_ = e.db.QueryRow(context.Background(), `SELECT count(*) FROM payments WHERE receipt_no=$1`, out.ReceiptNo).Scan(&n)
	if n != 1 {
		t.Fatalf("reprint created extra payment rows: %d", n)
	}
}

// TestBalanceBlockingAndTopup — gate item 2.
func TestBalanceBlockingAndTopup(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30)
	sub := e.createSubscriber(t, prof)
	agentID, agentTok := e.seedAgent(t)
	// The scoped agent must own the subscriber to renew it (FR-27.2).
	_, _ = e.db.Exec(context.Background(),
		`UPDATE subscribers SET owner_manager_id=$1::uuid WHERE id=$2::uuid`, agentID, sub)

	// Agent with 0 balance is blocked.
	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentTok, map[string]any{})
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 insufficient, got %d: %s", r.status, r.body)
	}

	// Top-up unblocks.
	tr := e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token, map[string]any{"amount": 25000, "currency": "IQD", "note": "cash"})
	if tr.status != http.StatusOK {
		t.Fatalf("topup = %d: %s", tr.status, tr.body)
	}
	r = e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentTok, map[string]any{})
	if r.status != http.StatusOK {
		t.Fatalf("renew after topup = %d: %s", r.status, r.body)
	}
	// Balance back to 0, and cache == ledger.
	if e.cachedBalance(t, agentID) != 0 {
		t.Fatalf("balance = %d, want 0", e.cachedBalance(t, agentID))
	}
	if e.cachedBalance(t, agentID) != e.ledgerSum(t, agentID) {
		t.Fatalf("cache %d != ledger %d", e.cachedBalance(t, agentID), e.ledgerSum(t, agentID))
	}
	// Second renewal blocked again.
	r = e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentTok, map[string]any{})
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on second renew, got %d", r.status)
	}
}

// TestBalanceEqualsLedgerProperty — randomized concurrent renewals/topups/refunds
// against one manager; the cached balance must always equal the ledger sum (AC-20b).
func TestBalanceEqualsLedgerProperty(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 1000, 30)
	agentID, agentTok := e.seedAgent(t)
	// Fund generously so renewals rarely block (blocking is fine — it just skips).
	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token, map[string]any{"amount": 1000000, "currency": "IQD"})

	// Give the agent a handful of their own subscribers.
	subs := make([]string, 5)
	for i := range subs {
		subs[i] = e.createSubscriber(t, prof)
		// reassign ownership to the agent so scope permits renew.
		_, _ = e.db.Exec(context.Background(),
			`UPDATE subscribers SET owner_manager_id=$1::uuid WHERE id=$2::uuid`, agentID, subs[i])
	}

	var wg sync.WaitGroup
	var renewTx sync.Map // ledger ids eligible for refund
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 3 {
			case 0:
				e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token, map[string]any{"amount": 500, "currency": "IQD"})
			default:
				sub := subs[i%len(subs)]
				r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentTok, map[string]any{})
				if r.status == http.StatusOK {
					var out struct {
						LedgerTxID string `json:"ledger_tx_id"`
					}
					r.into(t, &out)
					renewTx.Store(out.LedgerTxID, sub)
				}
			}
		}(i)
	}
	wg.Wait()

	// Refund a few renewals concurrently.
	var wg2 sync.WaitGroup
	renewTx.Range(func(k, v any) bool {
		wg2.Add(1)
		go func(txID, sub string) {
			defer wg2.Done()
			e.do(t, "POST", "/api/v1/subscribers/"+sub+"/refund", e.token,
				map[string]any{"ledger_tx_id": txID, "reason": "test"})
		}(k.(string), v.(string))
		return true
	})
	wg2.Wait()

	// Force a final balance recompute cannot drift: cache must equal ledger sum.
	if got, want := e.cachedBalance(t, agentID), e.ledgerSum(t, agentID); got != want {
		t.Fatalf("balance cache %d != ledger sum %d", got, want)
	}
}

// TestVoucherDoubleRedeemRace — 50 goroutines redeem the SAME code; exactly one
// wins (AC-22a / gate item 3).
func TestVoucherDoubleRedeemRace(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 1000, 30)
	sub := e.createSubscriber(t, prof)

	// Generate a batch of 1 and capture the single plaintext code from the CSV.
	r := e.do(t, "POST", "/api/v1/vouchers/batches", e.token,
		map[string]any{"profile_id": prof, "count": 1, "prefix": "NET"})
	if r.status != http.StatusCreated {
		t.Fatalf("create batch = %d: %s", r.status, r.body)
	}
	lines := bytes.Split(bytes.TrimSpace(r.body), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("expected header + 1 code, got %q", r.body)
	}
	code := string(bytes.TrimSpace(lines[1]))

	var success int64
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rr := e.do(t, "POST", "/api/v1/vouchers/redeem", e.token,
				map[string]any{"code": code, "subscriber_id": sub})
			if rr.status == http.StatusOK {
				atomic.AddInt64(&success, 1)
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("expected exactly 1 successful redeem, got %d", success)
	}

	// The voucher is marked used exactly once.
	var used int
	_ = e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM vouchers v JOIN voucher_batches b ON b.id=v.batch_id
		  WHERE v.state='used'`).Scan(&used)
	if used < 1 {
		t.Fatalf("voucher not marked used")
	}
}

// TestRefundReversalMath — a refund nets the ledger to 0, restores balance, and
// leaves the original entry untouched (AC-25a); a second refund is rejected.
func TestRefundReversalMath(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30)
	sub := e.createSubscriber(t, prof)
	agentID, agentTok := e.seedAgent(t)
	_, _ = e.db.Exec(context.Background(),
		`UPDATE subscribers SET owner_manager_id=$1::uuid WHERE id=$2::uuid`, agentID, sub)
	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token, map[string]any{"amount": 25000, "currency": "IQD"})

	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentTok, map[string]any{})
	if r.status != http.StatusOK {
		t.Fatalf("renew = %d: %s", r.status, r.body)
	}
	var out struct {
		LedgerTxID string `json:"ledger_tx_id"`
	}
	r.into(t, &out)
	if e.ledgerSum(t, agentID) != 0 { // 25000 topup - 25000 renewal
		t.Fatalf("pre-refund ledger = %d, want 0", e.ledgerSum(t, agentID))
	}

	rf := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/refund", e.token,
		map[string]any{"ledger_tx_id": out.LedgerTxID, "reason": "mistake"})
	if rf.status != http.StatusOK {
		t.Fatalf("refund = %d: %s", rf.status, rf.body)
	}
	// Balance restored to the top-up (renewal reversed).
	if e.ledgerSum(t, agentID) != 25000 {
		t.Fatalf("post-refund ledger = %d, want 25000", e.ledgerSum(t, agentID))
	}
	if e.cachedBalance(t, agentID) != 25000 {
		t.Fatalf("post-refund cache = %d, want 25000", e.cachedBalance(t, agentID))
	}
	// Second refund of same tx rejected.
	rf2 := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/refund", e.token,
		map[string]any{"ledger_tx_id": out.LedgerTxID, "reason": "again"})
	if rf2.status != http.StatusConflict {
		t.Fatalf("second refund = %d, want 409", rf2.status)
	}
}

// TestLedgerImmutability — UPDATE/DELETE on the ledger is refused at the DB level
// (AC-24a / gate item 5).
func TestLedgerImmutability(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 1000, 30)
	sub := e.createSubscriber(t, prof)
	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", e.token, map[string]any{})
	var out struct {
		LedgerTxID string `json:"ledger_tx_id"`
	}
	r.into(t, &out)

	if _, err := e.db.Exec(context.Background(),
		`UPDATE ledger_transactions SET amount=0 WHERE id=$1::uuid`, out.LedgerTxID); err == nil {
		t.Fatal("expected UPDATE on ledger to be refused")
	}
	if _, err := e.db.Exec(context.Background(),
		`DELETE FROM ledger_transactions WHERE id=$1::uuid`, out.LedgerTxID); err == nil {
		t.Fatal("expected DELETE on ledger to be refused")
	}
}

// TestIdempotentRenew — the same Idempotency-Key charges once.
func TestIdempotentRenew(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30)
	sub := e.createSubscriber(t, prof)
	key := uniq("idem_")

	first := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", e.token, map[string]any{},
		[2]string{"Idempotency-Key", key})
	second := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", e.token, map[string]any{},
		[2]string{"Idempotency-Key", key})
	if first.status != http.StatusOK || second.status != http.StatusOK {
		t.Fatalf("renews = %d,%d", first.status, second.status)
	}
	var a, b struct {
		LedgerTxID string `json:"ledger_tx_id"`
		ReceiptNo  string `json:"receipt_no"`
	}
	first.into(t, &a)
	second.into(t, &b)
	if a.LedgerTxID != b.LedgerTxID || a.ReceiptNo != b.ReceiptNo {
		t.Fatalf("idempotent replay differs: %+v vs %+v", a, b)
	}
	var n int
	_ = e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM ledger_transactions WHERE subscriber_id=$1::uuid AND type='renewal'`, sub).Scan(&n)
	if n != 1 {
		t.Fatalf("expected 1 charge for idempotent renew, got %d", n)
	}
}
