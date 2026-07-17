package importer_test

// DB-backed importer suite (gated on HIKRAD_TEST_DB_URL — see billing/db_test.go
// for the repo pattern). Boots the full router so upload/map/dry-run/execute
// exercise the real subscribers API (self-dispatched by the importer package
// under test), auth, and audit logging. Covers Phase-5 gate item 5: SAS4-shaped
// CSV (Arabic names, CP1256) -> dry-run catches planted errors with zero
// writes -> execute imports valid rows -> re-execute skips them.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/text/encoding/charmap"

	_ "github.com/hikrad/hikrad/internal/auth"
	_ "github.com/hikrad/hikrad/internal/importer"
	_ "github.com/hikrad/hikrad/internal/live"
	_ "github.com/hikrad/hikrad/internal/profiles"
	_ "github.com/hikrad/hikrad/internal/radius"
	_ "github.com/hikrad/hikrad/internal/subscribers"
)

type env struct {
	srv   *httptest.Server
	db    *pgxpool.Pool
	token string
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
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping importer DB suite")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 23)
	}
	redisURL := os.Getenv("HIKRAD_TEST_REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://unused:6379/0"
	}
	t.Setenv("HIKRAD_DB_URL", dbURL)
	t.Setenv("HIKRAD_REDIS_URL", redisURL)
	t.Setenv("HIKRAD_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("HIKRAD_JWT_SECRET", "importer-db-test-secret")
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
	if _, err := db.Exec(ctx,
		`INSERT INTO managers (username, password_hash, role) VALUES ($1,$2,'admin')`,
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

func (e env) createProfile(t *testing.T, name string, price int64, days int) string {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/profiles", e.token, map[string]any{
		"name": name, "price": price, "currency": "IQD", "duration_days": days,
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

// upload posts a base64-in-JSON CSV upload (contract C3's shape — see
// importer/api.go's uploadRequest comment for why not multipart) with the
// given raw bytes and optional preset, returning the parsed response.
func (e env) upload(t *testing.T, filename string, raw []byte, preset string) map[string]any {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/import/subscribers", e.token, map[string]any{
		"filename": filename, "content_base64": base64.StdEncoding.EncodeToString(raw), "preset": preset,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("upload = %d: %s", r.status, r.body)
	}
	var out map[string]any
	r.into(t, &out)
	return out
}

func (e env) waitExecuted(t *testing.T, batchID string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		r := e.do(t, "GET", "/api/v1/import/"+batchID, e.token, nil)
		if r.status != http.StatusOK {
			t.Fatalf("batch status = %d: %s", r.status, r.body)
		}
		var out map[string]any
		r.into(t, &out)
		if out["status"] == "completed" {
			return out
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("execute did not complete within the deadline")
	return nil
}

// --- tests ---------------------------------------------------------------

// TestImportSAS4FlowWithPlantedErrorsAndCP1256 — gate item 5: an SAS4-shaped
// CSV (Windows-1256 encoded, Arabic names) with duplicate-in-file, missing
// password, unknown profile, bad phone and bad expiry planted rows. dry-run
// must catch every one with zero writes; execute must create only the valid
// rows and preserve the Arabic name through the CP1256 -> UTF-8 decode.
func TestImportSAS4FlowWithPlantedErrorsAndCP1256(t *testing.T) {
	e := setup(t)
	profID := e.createProfile(t, uniq("Package_"), 10_000, 30)

	goodUser := uniq("sas4_good_")
	dupUser := uniq("sas4_dup_")
	arabicName := "أحمد علي"

	// Profile name is randomized per test run (avoids cross-test collisions);
	// resolve it before building rows so the "known good" row references a
	// real package.
	pkgName := e.profileName(t, profID)

	header := "UserName,Password,FullName,Mobile,Package,ExpireDate\n"
	rows := []string{
		fmt.Sprintf("%s,pass123,%s,07701234567,%s,2027-01-01", goodUser, arabicName, pkgName),
		fmt.Sprintf("%s,pass123,Dup One,,%s,2027-01-01", dupUser, pkgName),
		fmt.Sprintf("%s,pass123,Dup Two,,%s,2027-01-01", dupUser, pkgName), // duplicate within file
		fmt.Sprintf("%s,,No Password,,%s,2027-01-01", uniq("sas4_nopw_"), pkgName),
		fmt.Sprintf("%s,pass123,Bad Profile,,%s,2027-01-01", uniq("sas4_badprof_"), "NoSuchPackage"),
		fmt.Sprintf("%s,pass123,Bad Phone,123,%s,2027-01-01", uniq("sas4_badphone_"), pkgName),
		fmt.Sprintf("%s,pass123,Bad Expiry,,%s,not-a-date", uniq("sas4_badexp_"), pkgName),
	}
	final := header
	for _, r := range rows {
		final += r + "\n"
	}

	cp1256, err := charmap.Windows1256.NewEncoder().String(final)
	if err != nil {
		t.Fatalf("encode cp1256 fixture: %v", err)
	}

	up := e.upload(t, "sas4_export.csv", []byte(cp1256), "sas4")
	if up["encoding"] != "cp1256" {
		t.Fatalf("upload encoding = %v, want cp1256", up["encoding"])
	}
	batchID, _ := up["batch_id"].(string)
	if batchID == "" {
		t.Fatal("no batch_id in upload response")
	}
	if up["status"] != "mapped" {
		t.Fatalf("upload with sas4 preset status = %v, want mapped", up["status"])
	}

	// --- dry-run: zero writes, planted errors caught ---
	dr := e.do(t, "POST", "/api/v1/import/"+batchID+"/dry-run", e.token, map[string]any{})
	if dr.status != http.StatusOK {
		t.Fatalf("dry-run = %d: %s", dr.status, dr.body)
	}
	var drOut struct {
		Rows []struct {
			Row      int               `json:"row"`
			Fields   map[string]string `json:"fields"`
			Errors   []string          `json:"errors"`
			Warnings []string          `json:"warnings"`
			Action   string            `json:"action"`
		} `json:"rows"`
		WillCreate int `json:"will_create"`
		WillSkip   int `json:"will_skip"`
	}
	dr.into(t, &drOut)
	if len(drOut.Rows) != len(rows) {
		t.Fatalf("dry-run returned %d rows, want %d", len(drOut.Rows), len(rows))
	}

	// Zero writes: no subscriber exists yet for any planted username.
	var count int
	if err := e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM subscribers WHERE username = $1::citext`, goodUser).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("dry-run wrote a subscriber row (count=%d), must write zero", count)
	}

	byAction := map[string]int{}
	var arabicRow map[string]string
	dupErrors, noPwErrors, badProfErrors, badPhoneErrors, badExpErrors := 0, 0, 0, 0, 0
	for _, rr := range drOut.Rows {
		byAction[rr.Action]++
		if rr.Fields["username"] == goodUser {
			arabicRow = rr.Fields
		}
		for _, e := range rr.Errors {
			switch {
			case rr.Fields["username"] == dupUser && strings.Contains(e, "duplicate username within this file"):
				dupErrors++
			case strings.Contains(e, "password is required"):
				noPwErrors++
			case strings.Contains(e, "unknown profile"):
				badProfErrors++
			case strings.Contains(e, "not a valid Iraqi mobile number"):
				badPhoneErrors++
			case strings.Contains(e, "expiry"):
				badExpErrors++
			}
		}
	}
	if dupErrors < 1 {
		t.Error("expected a duplicate-within-file error on the second dup row")
	}
	if noPwErrors != 1 {
		t.Errorf("noPwErrors = %d, want 1", noPwErrors)
	}
	if badProfErrors != 1 {
		t.Errorf("badProfErrors = %d, want 1", badProfErrors)
	}
	if badPhoneErrors != 1 {
		t.Errorf("badPhoneErrors = %d, want 1", badPhoneErrors)
	}
	if badExpErrors != 1 {
		t.Errorf("badExpErrors = %d, want 1", badExpErrors)
	}
	if arabicRow == nil {
		t.Fatal("could not find the good row in dry-run output")
	}
	if arabicRow["name"] != arabicName {
		t.Fatalf("Arabic name round-trip: got %q, want %q", arabicRow["name"], arabicName)
	}
	// Exactly 2 rows should be eligible to create: goodUser and the FIRST dup
	// occurrence (dupUser's second occurrence is the one flagged as a
	// duplicate-within-file error).
	if drOut.WillCreate != 2 {
		t.Fatalf("will_create = %d, want 2 (good row + first dup occurrence)", drOut.WillCreate)
	}

	// --- execute: only valid rows created, idempotent re-run skips them ---
	ex := e.do(t, "POST", "/api/v1/import/"+batchID+"/execute", e.token, map[string]any{})
	if ex.status != http.StatusAccepted {
		t.Fatalf("execute = %d: %s", ex.status, ex.body)
	}
	final1 := e.waitExecuted(t, batchID)
	summary1, _ := final1["summary"].(map[string]any)
	if summary1["created"] != float64(2) {
		t.Fatalf("execute summary.created = %v, want 2 (%v)", summary1["created"], final1)
	}

	var sub struct {
		Name string
	}
	if err := e.db.QueryRow(context.Background(),
		`SELECT COALESCE(name,'') FROM subscribers WHERE username = $1::citext`, goodUser).Scan(&sub.Name); err != nil {
		t.Fatalf("query created subscriber: %v", err)
	}
	if sub.Name != arabicName {
		t.Fatalf("created subscriber name = %q, want %q (CP1256 Arabic must survive the whole pipeline)", sub.Name, arabicName)
	}

	// Audit + policy invalidation "come free" via the real create handler:
	// an audit_log row for subscriber.create must exist for this user.
	var auditCount int
	if err := e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = 'subscriber.create' AND entity_id IN
		   (SELECT id::text FROM subscribers WHERE username = $1::citext)`, goodUser).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount < 1 {
		t.Fatal("no audit_log entry for the imported subscriber — execute must go through the real create API, not raw SQL")
	}

	// Idempotent re-execute: batch already 'completed'; a second execute call
	// must create 0 new rows (both eligible rows already imported).
	ex2 := e.do(t, "POST", "/api/v1/import/"+batchID+"/execute", e.token, map[string]any{})
	if ex2.status != http.StatusAccepted {
		t.Fatalf("re-execute = %d: %s", ex2.status, ex2.body)
	}
	final2 := e.waitExecuted(t, batchID)
	summary2, _ := final2["summary"].(map[string]any)
	if summary2["created"] != float64(0) {
		t.Fatalf("re-execute summary.created = %v, want 0 (idempotent)", summary2["created"])
	}
}

func (e env) profileName(t *testing.T, id string) string {
	t.Helper()
	var name string
	if err := e.db.QueryRow(context.Background(), `SELECT name FROM profiles WHERE id = $1::uuid`, id).Scan(&name); err != nil {
		t.Fatal(err)
	}
	return name
}
