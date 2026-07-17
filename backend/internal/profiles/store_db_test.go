package profiles

// DB-backed tests for the delete guard (gated on HIKRAD_TEST_DB_URL, matching
// the repo pattern in internal/auth and internal/subscribers).
//
// Why this file exists: profileInUse hand-writes a column list against
// tables owned by other phases (subscribers 0100, voucher_batches 0202,
// payment_tickets 0583 — v2-2's generalization of the retired
// payment_intents 0302/card_payments 0304). Nothing in the package ever
// executed that SQL, so a wrong column name was indistinguishable from a
// correct one until an operator hit it in production — which is exactly what
// happened: it read vouchers.profile_id, but a voucher inherits its plan from
// its BATCH, so every call raised 42703 and every profile delete 500'd.
//
// A guard that cannot execute is worse than no guard: it fails closed on the
// deletes it should allow and never once evaluates the reference it exists to
// find. TestProfileInUse_QueryExecutes is the cheap regression test for that
// whole class — it asserts only that the statement runs.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"testing"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping profiles DB suite")
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if err := platform.Migrate(dbURL, "../../migrations", log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := platform.NewDB(context.Background(), platform.Config{DBURL: dbURL})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

// uniqName keeps rows isolated on a shared CI database.
func uniqName(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

// newProfile inserts a bare plan and returns its id.
func newProfile(t *testing.T, db *pgxpool.Pool, name string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(context.Background(),
		`INSERT INTO profiles (name, price, duration_days, rate_down_kbps, rate_up_kbps)
		 VALUES ($1, 1000, 30, 2048, 1024) RETURNING id::text`, name).Scan(&id); err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	return id
}

// newSubscriber inserts a subscriber on the given plan and returns its id.
func newSubscriber(t *testing.T, db *pgxpool.Pool, profileID string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(context.Background(),
		`INSERT INTO subscribers (username, password_enc, profile_id)
		 VALUES ($1, '\x00'::bytea, $2::uuid) RETURNING id::text`,
		uniqName("sub"), profileID).Scan(&id); err != nil {
		t.Fatalf("insert subscriber: %v", err)
	}
	return id
}

func mustExec(t *testing.T, db *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := db.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %s: %v", sql, err)
	}
}

// TestProfileInUse_QueryExecutes is the regression test for the 42703 bug: the
// guard must RUN. A profile nothing references must answer false, not error.
func TestProfileInUse_QueryExecutes(t *testing.T) {
	db := testDB(t)
	id := newProfile(t, db, uniqName("unused"))

	inUse, err := profileInUse(context.Background(), db, id)
	if err != nil {
		t.Fatalf("profileInUse must execute against the real schema; got error: %v", err)
	}
	if inUse {
		t.Fatal("a profile nothing references reported in-use")
	}
}

// TestProfileInUse_FindsEachReference proves every table in the guard is
// spelled and joined correctly by planting exactly ONE real reference at a time
// on an otherwise-unreferenced plan. The voucher case is the one that
// regressed: the plan is on voucher_batches, and a voucher points at the batch.
//
// The payment/card rows need a subscriber, and a subscriber needs a plan — but
// it must be a DIFFERENT plan (`filler`), or that subscriber alone would make
// the plan under test in-use and the subtest would pass no matter how the
// payment table were spelled. That is the bug this file exists to catch, so the
// test must not reproduce it.
func TestProfileInUse_FindsEachReference(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	filler := newProfile(t, db, uniqName("filler"))

	for _, tc := range []struct {
		name string
		ref  func(t *testing.T, profileID string)
	}{
		{"subscriber on the plan", func(t *testing.T, profileID string) {
			newSubscriber(t, db, profileID)
		}},
		{"subscriber scheduled to move to the plan", func(t *testing.T, profileID string) {
			mustExec(t, db, `INSERT INTO subscribers (username, password_enc, profile_id, pending_profile_id)
			                 VALUES ($1, '\x00'::bytea, $2::uuid, $3::uuid)`,
				uniqName("sub"), filler, profileID)
		}},
		{"voucher batch sold on the plan", func(t *testing.T, profileID string) {
			mustExec(t, db, `INSERT INTO voucher_batches (profile_id, count, unit_price)
			                 VALUES ($1::uuid, 5, 1000)`, profileID)
		}},
		{"payment ticket for the plan", func(t *testing.T, profileID string) {
			mustExec(t, db, `INSERT INTO payment_tickets (subscriber_id, profile_id, method_key, amount, currency)
			                 VALUES ($1::uuid, $2::uuid, 'scratch_card', 1000, 'IQD')`,
				newSubscriber(t, db, filler), profileID)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := newProfile(t, db, uniqName("used"))

			// Sanity: unreferenced first, so a subtest that plants nothing
			// useful cannot pass on a pre-existing reference.
			if inUse, err := profileInUse(ctx, db, id); err != nil || inUse {
				t.Fatalf("fresh plan must start unreferenced (in_use=%v err=%v)", inUse, err)
			}
			tc.ref(t, id)

			inUse, err := profileInUse(ctx, db, id)
			if err != nil {
				t.Fatalf("profileInUse: %v", err)
			}
			if !inUse {
				t.Fatal("a referenced plan reported deletable; deleting it would rewrite what was already sold")
			}
		})
	}
}
