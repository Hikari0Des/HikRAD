package push

// DB-backed suite (gated on HIKRAD_TEST_DB_URL, matching the repo pattern):
// subscription upsert/dedup-by-endpoint, owner-scoped unsubscribe, and the
// real 410-Gone prune path end to end against push_subscriptions (migration
// 0330). An internal (package push, not push_test) test file so it can set
// the package-level pkgDB/pkgSettings handles directly, the same way
// module.Register does at boot.

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping push DB suite")
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := platform.Migrate(dbURL, "../../migrations", log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := platform.NewDB(context.Background(), platform.Config{DBURL: dbURL})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)
	pkgDB = db
	pkgSettings = newFakeSettings()
	resetCache()
	return db
}

func randomTestUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func fakeKeys() Keys {
	curve := ecdh.P256()
	p, _ := curve.GenerateKey(rand.Reader)
	a := make([]byte, 16)
	_, _ = rand.Read(a)
	return Keys{
		P256dh: base64.RawURLEncoding.EncodeToString(p.PublicKey().Bytes()),
		Auth:   base64.RawURLEncoding.EncodeToString(a),
	}
}

// A re-subscribe from the same browser (same endpoint) must upsert in place,
// never duplicate (edge case in the task brief).
func TestSubscribe_DedupesByEndpoint(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	managerID := randomTestUUID()
	endpoint := "https://push.example.com/ep/" + randomTestUUID()
	if err := Subscribe(ctx, "panel", managerID, endpoint, fakeKeys()); err != nil {
		t.Fatalf("Subscribe (first): %v", err)
	}
	keys2 := fakeKeys()
	if err := Subscribe(ctx, "panel", managerID, endpoint, keys2); err != nil {
		t.Fatalf("Subscribe (re-subscribe): %v", err)
	}

	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM push_subscriptions WHERE endpoint = $1`, endpoint).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row for a re-subscribed endpoint, got %d", count)
	}
	var storedP256dh string
	if err := db.QueryRow(ctx, `SELECT p256dh FROM push_subscriptions WHERE endpoint = $1`, endpoint).Scan(&storedP256dh); err != nil {
		t.Fatal(err)
	}
	if storedP256dh != keys2.P256dh {
		t.Fatal("re-subscribe should update the stored keys, not keep the stale ones")
	}
}

// Unsubscribe is scoped to the caller's own surface+owner — one manager must
// not be able to remove another's subscription.
func TestUnsubscribe_ScopedToOwner(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	owner := randomTestUUID()
	other := randomTestUUID()
	endpoint := "https://push.example.com/ep/" + randomTestUUID()
	if err := Subscribe(ctx, "panel", owner, endpoint, fakeKeys()); err != nil {
		t.Fatal(err)
	}
	if err := Unsubscribe(ctx, "panel", other, endpoint); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM push_subscriptions WHERE endpoint = $1`, endpoint).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("a different owner's unsubscribe must not remove someone else's subscription")
	}
	if err := Unsubscribe(ctx, "panel", owner, endpoint); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM push_subscriptions WHERE endpoint = $1`, endpoint).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("the real owner's unsubscribe should have removed the row")
	}
}

// The end-to-end 410 path: a gone endpoint is pruned from the DB, and a
// healthy sibling subscription for the same manager still receives delivery
// (delivery isolation, NFR-7).
func TestDeliverPanel_PrunesGoneSubscription(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	managerID := randomTestUUID()
	var goodHits int
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		goodHits++
		w.WriteHeader(http.StatusCreated)
	}))
	defer good.Close()
	gone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer gone.Close()

	goneEndpoint := gone.URL + "/ep/" + randomTestUUID()
	goodEndpoint := good.URL + "/ep/" + randomTestUUID()
	if err := Subscribe(ctx, "panel", managerID, goneEndpoint, fakeKeys()); err != nil {
		t.Fatal(err)
	}
	if err := Subscribe(ctx, "panel", managerID, goodEndpoint, fakeKeys()); err != nil {
		t.Fatal(err)
	}

	if err := DeliverPanel(ctx, Payload{TitleKey: "t", BodyKey: "b"}); err != nil {
		t.Fatalf("DeliverPanel should succeed while at least one subscription is healthy: %v", err)
	}
	if goodHits != 1 {
		t.Fatalf("expected the healthy subscription to receive 1 push, got %d", goodHits)
	}

	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM push_subscriptions WHERE endpoint = $1`, goneEndpoint).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("a 410-Gone endpoint should have been pruned")
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM push_subscriptions WHERE endpoint = $1`, goodEndpoint).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("the healthy subscription must survive")
	}
}
