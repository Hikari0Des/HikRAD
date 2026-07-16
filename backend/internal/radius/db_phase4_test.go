package radius

// DB-backed auto-setup suite (gated on HIKRAD_TEST_DB_URL, matching the repo
// pattern — see internal/billing/db_test.go). Covers the C6 negative tests
// the Phase 4 task explicitly calls out: a planted conflicting /radius entry,
// the router's state changing between preview and apply, and wrong
// credentials — none of which need a real MikroTik/CHR, since the RouterOS
// API transport is a seam (module.dialROS) a fake in-memory router satisfies.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/platform/crypto"
	"github.com/hikrad/hikrad/internal/radius/vendor"
)

func autoSetupTestModule(t *testing.T) *module {
	t.Helper()
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping auto-setup DB suite")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 11)
	}
	if err := crypto.Configure(key); err != nil {
		t.Fatalf("configure crypto: %v", err)
	}
	if err := platform.Migrate(dbURL, "../../migrations", log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := platform.NewDB(ctx, platform.Config{DBURL: dbURL})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)

	return &module{db: db, log: log}
}

// uniqueTestIP avoids the nas.ip unique constraint colliding across repeated
// runs against a persistent (non-transactional) test database.
func uniqueTestIP(t *testing.T) string {
	t.Helper()
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("10.%d.%d.%d", b[0], b[1], b[2]|1)
}

// mustInsertNAS seeds a NAS row through the real insertNAS path (dogfoods the
// production insert, including AES-GCM sealing of the API password).
func mustInsertNAS(t *testing.T, m *module, apiUser, apiPassword, rosVersion string) nasRow {
	t.Helper()
	ip := uniqueTestIP(t)
	ros := rosVersion
	in := nasInput{
		Name: "autosetup-test-" + ip, IP: ip, Secret: "radsecret",
		Vendor: "mikrotik", CoAPort: 3799, Enabled: true, APIUser: apiUser, APIPassword: apiPassword,
	}
	if ros != "" {
		in.ROSVersion = &ros
	}
	ctx := context.Background()
	n, err := insertNAS(ctx, m.db, in)
	if err != nil {
		t.Fatalf("insert nas: %v", err)
	}
	// Every NAS keeps >= 1 service instance (C3); these fixtures are the PPPoE
	// single-service shape v1's Type:"pppoe" used to mean.
	if _, err := m.db.Exec(ctx,
		`INSERT INTO nas_services (nas_id, service, label, enabled) VALUES ($1::uuid, 'pppoe', 'test-pppoe', true)`,
		n.ID); err != nil {
		t.Fatalf("insert nas service: %v", err)
	}
	return n
}

// fakeRouter is an in-memory RouterOS device satisfying vendor.ROSConn — no
// network, no CHR image, safe for parallel access across the module's own
// concurrency guarantees.
type fakeRouter struct {
	mu        sync.Mutex
	rows      map[string][]map[string]string
	writes    [][]string
	failWrite bool
}

func newFakeRouter() *fakeRouter { return &fakeRouter{rows: map[string][]map[string]string{}} }

func (f *fakeRouter) Read(sentence ...string) ([]map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rows[sentence[0]], nil
}

func (f *fakeRouter) Write(sentence ...string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWrite {
		return nil, errors.New("simulated router write failure")
	}
	f.writes = append(f.writes, sentence)
	f.applyLocked(sentence)
	return map[string]string{}, nil
}

func (f *fakeRouter) Close() error { return nil }

// applyLocked simulates the router persisting a write into the state a
// subsequent /print would report, so a second preview against the same
// fakeRouter proves the plan is idempotent end to end (not just that a
// hand-seeded "already applied" row map short-circuits, which the vendor
// package's own unit tests already cover). Caller holds f.mu.
func (f *fakeRouter) applyLocked(sentence []string) {
	if len(sentence) == 0 {
		return
	}
	path := sentence[0]
	args := map[string]string{}
	for _, w := range sentence[1:] {
		w = strings.TrimPrefix(w, "=")
		if i := strings.IndexByte(w, '='); i >= 0 {
			args[w[:i]] = w[i+1:]
		}
	}
	switch {
	case strings.HasSuffix(path, "/add"):
		printPath := strings.TrimSuffix(path, "/add") + "/print"
		delete(args, ".id")
		f.rows[printPath] = append(f.rows[printPath], args)
	case strings.HasSuffix(path, "/set"):
		printPath := strings.TrimSuffix(path, "/set") + "/print"
		id, hasID := args[".id"]
		delete(args, ".id")
		if hasID {
			for i, row := range f.rows[printPath] {
				if row[".id"] == id {
					for k, v := range args {
						f.rows[printPath][i][k] = v
					}
					return
				}
			}
			return
		}
		if len(f.rows[printPath]) == 0 {
			f.rows[printPath] = []map[string]string{args}
			return
		}
		for k, v := range args {
			f.rows[printPath][0][k] = v
		}
	}
}

func (f *fakeRouter) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.writes)
}

func dialFakeRouter(r *fakeRouter) func(context.Context, string, int, string, string) (vendor.ROSConn, error) {
	return func(context.Context, string, int, string, string) (vendor.ROSConn, error) { return r, nil }
}

// --- request/response plumbing ---------------------------------------------

func autoSetupRequest(t *testing.T, method, path, nasID string, body any) *http.Request {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", nasID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func decodeJSON[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return out
}

// --- tests -------------------------------------------------------------

func TestAutoSetup_NoCredentials_Returns422(t *testing.T) {
	m := autoSetupTestModule(t)
	n := mustInsertNAS(t, m, "", "", "7")

	rec := httptest.NewRecorder()
	m.autoSetupPreviewHandler(rec, autoSetupRequest(t, "POST", "/preview", n.ID, nil))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("preview with no credentials = %d, want 422: %s", rec.Code, rec.Body)
	}
}

func TestAutoSetup_WrongCredentials_Returns502(t *testing.T) {
	m := autoSetupTestModule(t)
	m.dialROS = func(context.Context, string, int, string, string) (vendor.ROSConn, error) {
		return nil, errors.New("RouterOS: /login: invalid user name or password")
	}
	n := mustInsertNAS(t, m, "admin", "wrongpass", "7")

	rec := httptest.NewRecorder()
	m.autoSetupPreviewHandler(rec, autoSetupRequest(t, "POST", "/preview", n.ID, nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("preview with wrong credentials = %d, want 502: %s", rec.Code, rec.Body)
	}
}

func TestAutoSetup_UnvalidatedROSVersion_ApplyRefused(t *testing.T) {
	m := autoSetupTestModule(t)
	// dialROS deliberately left nil: the version gate must refuse before ever
	// trying to connect, or this test panics on the nil call instead of
	// quietly passing.
	n := mustInsertNAS(t, m, "admin", "pass", "5.26") // pre-6.49, not in the matrix

	rec := httptest.NewRecorder()
	m.autoSetupApplyHandler(rec, autoSetupRequest(t, "POST", "/apply", n.ID, map[string]string{"preview_hash": "whatever"}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("apply on unvalidated ROS version = %d, want 409: %s", rec.Code, rec.Body)
	}
}

func TestAutoSetup_PlantedConflict_AbortsApplyRouterUnchanged(t *testing.T) {
	m := autoSetupTestModule(t)
	router := newFakeRouter()
	router.rows["/radius/print"] = []map[string]string{
		{"address": "RADIUS_SERVER_IP", "service": "ppp", "secret": "someone-elses-secret", "comment": "manually configured"},
	}
	m.dialROS = dialFakeRouter(router)
	n := mustInsertNAS(t, m, "admin", "pass", "7")

	rec := httptest.NewRecorder()
	m.autoSetupPreviewHandler(rec, autoSetupRequest(t, "POST", "/preview", n.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", rec.Code, rec.Body)
	}
	preview := decodeJSON[autoSetupPreviewResponse](t, rec)
	if len(preview.Conflicts) == 0 {
		t.Fatalf("expected the planted foreign /radius entry to surface as a conflict, got %+v", preview)
	}

	applyRec := httptest.NewRecorder()
	m.autoSetupApplyHandler(applyRec, autoSetupRequest(t, "POST", "/apply", n.ID, map[string]string{"preview_hash": preview.PreviewHash}))
	if applyRec.Code != http.StatusConflict {
		t.Fatalf("apply with an unresolved conflict = %d, want 409: %s", applyRec.Code, applyRec.Body)
	}
	if got := router.writeCount(); got != 0 {
		t.Fatalf("router must be untouched when apply aborts on conflict; got %d write(s): %+v", got, router.writes)
	}
}

func TestAutoSetup_RouterStateChangedSincePreview_HashMismatchAborts(t *testing.T) {
	m := autoSetupTestModule(t)
	router := newFakeRouter() // starts empty: preview sees a clean, all-additive plan
	m.dialROS = dialFakeRouter(router)
	n := mustInsertNAS(t, m, "admin", "pass", "7")

	rec := httptest.NewRecorder()
	m.autoSetupPreviewHandler(rec, autoSetupRequest(t, "POST", "/preview", n.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", rec.Code, rec.Body)
	}
	preview := decodeJSON[autoSetupPreviewResponse](t, rec)
	if len(preview.Conflicts) != 0 {
		t.Fatalf("expected a clean preview on an empty router, got conflicts %+v", preview.Conflicts)
	}

	// Someone/something changes the router between preview and apply — e.g. a
	// competing tool adds its own /radius entry.
	router.rows["/radius/print"] = []map[string]string{
		{"address": "RADIUS_SERVER_IP", "service": "ppp", "secret": "different", "comment": "not hikrad"},
	}

	applyRec := httptest.NewRecorder()
	m.autoSetupApplyHandler(applyRec, autoSetupRequest(t, "POST", "/apply", n.ID, map[string]string{"preview_hash": preview.PreviewHash}))
	if applyRec.Code != http.StatusConflict {
		t.Fatalf("apply after router state changed = %d, want 409 (hash mismatch): %s", applyRec.Code, applyRec.Body)
	}
	if got := router.writeCount(); got != 0 {
		t.Fatalf("router must be untouched when the preview hash is stale; got %d write(s)", got)
	}
}

func TestAutoSetup_HappyPath_AppliesAdditiveItems(t *testing.T) {
	m := autoSetupTestModule(t)
	router := newFakeRouter()
	m.dialROS = dialFakeRouter(router)
	n := mustInsertNAS(t, m, "admin", "pass", "7")

	rec := httptest.NewRecorder()
	m.autoSetupPreviewHandler(rec, autoSetupRequest(t, "POST", "/preview", n.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", rec.Code, rec.Body)
	}
	preview := decodeJSON[autoSetupPreviewResponse](t, rec)
	if len(preview.Items) == 0 || len(preview.Conflicts) != 0 {
		t.Fatalf("expected a non-empty, conflict-free plan on a fresh router, got %+v", preview)
	}

	applyRec := httptest.NewRecorder()
	m.autoSetupApplyHandler(applyRec, autoSetupRequest(t, "POST", "/apply", n.ID, map[string]string{"preview_hash": preview.PreviewHash}))
	if applyRec.Code != http.StatusOK {
		t.Fatalf("apply = %d: %s", applyRec.Code, applyRec.Body)
	}
	applied := decodeJSON[autoSetupApplyResponse](t, applyRec)
	if !applied.AllOK {
		t.Fatalf("expected every item to apply cleanly, got %+v", applied.Results)
	}
	if len(applied.Results) != len(preview.Items) {
		t.Fatalf("applied %d items, previewed %d", len(applied.Results), len(preview.Items))
	}
	if got := router.writeCount(); got != len(preview.Items) {
		t.Fatalf("router recorded %d writes, want %d", got, len(preview.Items))
	}

	// A second apply against the now-configured router (fresh preview first)
	// must be a no-op — proves idempotency end to end through the HTTP layer.
	rec2 := httptest.NewRecorder()
	m.autoSetupPreviewHandler(rec2, autoSetupRequest(t, "POST", "/preview", n.ID, nil))
	preview2 := decodeJSON[autoSetupPreviewResponse](t, rec2)
	if len(preview2.Items) != 0 || len(preview2.Conflicts) != 0 {
		t.Fatalf("expected a no-op plan after apply, got %+v", preview2)
	}
}
