package cards

// DB-backed suite (gated on HIKRAD_TEST_DB_URL, matching the repo pattern —
// see internal/profiles/store_db_test.go). Covers group CRUD, the group
// actions FR-136 names (enable/disable-all, extend, add-more, duplicate,
// per-card reset), and the full card lifecycle unused → active → exhausted
// via the REAL AuthenticateCard against real usage_points/sessions rows —
// gate item 6.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping cards DB suite")
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

func uniqName(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func TestCreateGroup_GeneratesRequestedCount(t *testing.T) {
	db := testDB(t)
	in := groupInput{Name: uniqName("grp"), Count: 5, TransferMode: "combined", TransferTotalBytes: ptr64(1000)}
	if fe := in.validate(); len(fe) != 0 {
		t.Fatalf("unexpected validation errors: %v", fe)
	}
	g, codes, err := createGroup(context.Background(), db, in, "", nil)
	if err != nil {
		t.Fatalf("createGroup: %v", err)
	}
	if len(codes) != 5 {
		t.Fatalf("codes = %d, want 5", len(codes))
	}
	if g.CardCount != 5 || g.UnusedCount != 5 {
		t.Fatalf("group counts = %+v, want 5 total/unused", g)
	}
	// Codes must be unique.
	seen := map[string]bool{}
	for _, c := range codes {
		if seen[c] {
			t.Fatalf("duplicate code generated: %s", c)
		}
		seen[c] = true
	}
}

func TestGroupActions_EnableDisableExtendAddMoreDuplicate(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	g, _ := mustCreateGroup(t, db, groupInput{Name: uniqName("grp"), Count: 2})

	if err := setGroupEnabled(ctx, db, g.ID, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, err := getGroup(ctx, db, g.ID)
	if err != nil || got.Enabled {
		t.Fatalf("group not disabled: enabled=%v err=%v", got.Enabled, err)
	}
	if err := setGroupEnabled(ctx, db, g.ID, true); err != nil {
		t.Fatalf("enable: %v", err)
	}

	shelf := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	if err := extendValidUntil(ctx, db, g.ID, shelf); err != nil {
		t.Fatalf("extend: %v", err)
	}
	got, _ = getGroup(ctx, db, g.ID)
	if got.ValidUntil == nil || !got.ValidUntil.Equal(shelf) {
		t.Fatalf("valid_until = %v, want %v", got.ValidUntil, shelf)
	}

	moreCodes, err := addMoreCards(ctx, db, g.ID, 3)
	if err != nil {
		t.Fatalf("add more: %v", err)
	}
	if len(moreCodes) != 3 {
		t.Fatalf("add-more codes = %d, want 3", len(moreCodes))
	}
	got, _ = getGroup(ctx, db, g.ID)
	if got.CardCount != 5 {
		t.Fatalf("card count after add-more = %d, want 5", got.CardCount)
	}

	dup, dupCodes, err := duplicateGroup(ctx, db, g.ID, 4)
	if err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	if dup.ID == g.ID {
		t.Fatal("duplicate returned the same group id")
	}
	if len(dupCodes) != 4 {
		t.Fatalf("duplicate codes = %d, want 4 (fresh, not copied from source)", len(dupCodes))
	}
	if dup.Name != g.Name+" (copy)" {
		t.Fatalf("duplicate name = %q, want %q", dup.Name, g.Name+" (copy)")
	}
	if dup.ValidUntil == nil || !dup.ValidUntil.Equal(shelf) {
		t.Fatalf("duplicate did not copy valid_until: got %v want %v", dup.ValidUntil, shelf)
	}
}

func TestResetCard_MovesAnchorAndRestoresExhausted(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	g, codes := mustCreateGroup(t, db, groupInput{
		Name: uniqName("grp"), Count: 1, TransferMode: "combined", TransferTotalBytes: ptr64(100),
	})
	list, err := listCards(ctx, db, g.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("listCards: %v (%d)", err, len(list))
	}
	cardID := list[0].ID

	auth := &cardAuthenticator{db: db}
	// First login activates the card.
	view, ok, err := auth.AuthenticateCard(ctx, codes[0])
	if err != nil || !ok {
		t.Fatalf("first auth: ok=%v err=%v", ok, err)
	}
	if view.QuotaExhausted {
		t.Fatal("should not be exhausted before any usage")
	}

	// Push usage past the 100-byte combined limit.
	if _, err := db.Exec(ctx,
		`INSERT INTO usage_points (time, card_id, nas_id, delta_down, delta_up, service)
		 VALUES (now(), $1::uuid, gen_random_uuid(), 60, 60, 'hotspot')`, cardID); err != nil {
		t.Fatalf("seed usage: %v", err)
	}
	view, ok, err = auth.AuthenticateCard(ctx, codes[0])
	if err != nil || !ok {
		t.Fatalf("second auth: ok=%v err=%v", ok, err)
	}
	if !view.QuotaExhausted {
		t.Fatal("expected quota exhausted after crossing the combined limit")
	}

	// Simulate the RADIUS-side reject having recorded the state as
	// exhausted (cards/auth.go itself never writes state; a future
	// enforcement pass or the panel's own state-refresh would) — directly
	// set it here so resetCard has something to restore.
	if err := setCardState(ctx, db, cardID, "exhausted"); err != nil {
		t.Fatalf("set exhausted: %v", err)
	}

	if err := resetCard(ctx, db, cardID); err != nil {
		t.Fatalf("reset: %v", err)
	}
	list, _ = listCards(ctx, db, g.ID)
	if list[0].State != "active" {
		t.Fatalf("state after reset = %q, want active", list[0].State)
	}
	if list[0].UsageDownBytes != 0 || list[0].UsageUpBytes != 0 {
		t.Fatalf("usage after reset = (%d,%d), want (0,0) — anchor should have moved forward",
			list[0].UsageDownBytes, list[0].UsageUpBytes)
	}
	// Post-reset, the card must authenticate again without being exhausted.
	view, ok, err = auth.AuthenticateCard(ctx, codes[0])
	if err != nil || !ok {
		t.Fatalf("post-reset auth: ok=%v err=%v", ok, err)
	}
	if view.QuotaExhausted {
		t.Fatal("still exhausted after reset — usage_cycle_anchor did not move")
	}
}

func TestCardLifecycle_ExpireAfterFirstLoginAndShelfDate(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	// expire_after_seconds = 1: the card expires essentially immediately
	// after first login, independent of any usage.
	one := int64(1)
	_, codes := mustCreateGroup(t, db, groupInput{Name: uniqName("grp"), Count: 1, ExpireAfterSeconds: &one})
	auth := &cardAuthenticator{db: db}
	view, ok, err := auth.AuthenticateCard(ctx, codes[0])
	if err != nil || !ok {
		t.Fatalf("first login: ok=%v err=%v", ok, err)
	}
	if view.ExpiresAt.IsZero() {
		t.Fatal("expected an expiry to be set from expire_after_seconds")
	}
	time.Sleep(1200 * time.Millisecond)
	view, ok, err = auth.AuthenticateCard(ctx, codes[0])
	if err != nil || !ok {
		t.Fatalf("second login: ok=%v err=%v", ok, err)
	}
	if !time.Now().UTC().After(view.ExpiresAt) {
		t.Fatalf("expected expiry to have passed: now=%v expires=%v", time.Now().UTC(), view.ExpiresAt)
	}
}

// TestCardLifecycle_ShelfDateIndependentOfPostLoginWindow proves gate item 7:
// a card inside its post-login expire_after_seconds window but past
// valid_until is refused (its effective expiry is the EARLIER of the two).
func TestCardLifecycle_ShelfDateIndependentOfPostLoginWindow(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	longWindow := int64(365 * 24 * 3600) // a year — nowhere near expiring on its own
	futureShelf := time.Now().Add(-1 * time.Hour).UTC()

	// Create with a generous window, then set valid_until in the PAST directly
	// (createGroup's input validate() would reject an obviously-past date from
	// a real client, but the column itself carries no such constraint — this
	// simulates "the shelf date arrived" for a card that already logged in).
	g, codes := mustCreateGroup(t, db, groupInput{Name: uniqName("grp"), Count: 1, ExpireAfterSeconds: &longWindow})
	auth := &cardAuthenticator{db: db}
	if _, ok, err := auth.AuthenticateCard(ctx, codes[0]); err != nil || !ok {
		t.Fatalf("first login: ok=%v err=%v", ok, err)
	}
	if err := extendValidUntil(ctx, db, g.ID, futureShelf); err != nil {
		t.Fatalf("set shelf date: %v", err)
	}

	view, ok, err := auth.AuthenticateCard(ctx, codes[0])
	if err != nil || !ok {
		t.Fatalf("second login: ok=%v err=%v", ok, err)
	}
	if !time.Now().UTC().After(view.ExpiresAt) {
		t.Fatalf("expected the shelf date to make this card already-expired: expires=%v", view.ExpiresAt)
	}
}

func ptr64(v int64) *int64 { return &v }

// mustCreateGroup runs the same validate()-then-create sequence api.go's
// createGroupHandler does — createGroup itself trusts the caller to have
// already normalized/validated the input (defaults like code_format are
// applied by validate(), not by createGroup), exactly like every other
// store-layer function in this codebase.
func mustCreateGroup(t *testing.T, db *pgxpool.Pool, in groupInput) (Group, []string) {
	t.Helper()
	if fe := in.validate(); len(fe) != 0 {
		t.Fatalf("validate: %v", fe)
	}
	g, codes, err := createGroup(context.Background(), db, in, "", nil)
	if err != nil {
		t.Fatalf("createGroup: %v", err)
	}
	return g, codes
}
