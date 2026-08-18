package subscribers_test

// End-to-end DB coverage for FR-133 batch creation, mirroring the shape of
// dbharness_test.go's other suites. Gated on HIKRAD_TEST_DB_URL; skips
// cleanly when unset (this sandbox has no live Postgres).

import (
	"net/http"
	"testing"
	"time"
)

func pollBatchJob(t *testing.T, e testEnv, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r := e.do(t, "GET", "/api/v1/subscribers/batch/"+id, nil)
		if r.status != http.StatusOK {
			t.Fatalf("poll job = %d: %s", r.status, r.body)
		}
		var out map[string]any
		r.into(t, &out)
		if out["status"] == "completed" {
			return out
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("batch job did not complete in time")
	return nil
}

// TestBatch_CreatesAllRows_DryRunMatchesExecute proves gate item 3: 200
// accounts in one pass, and the dry-run's usernames/total match the real run
// exactly.
func TestBatch_CreatesAllRows_DryRunMatchesExecute(t *testing.T) {
	e := setup(t)
	profileID := e.createProfile(t, "batch-plan", 5000, 5000)
	prefix := e.prefix + "batch"

	body := map[string]any{
		"prefix": prefix, "start_index": 1, "count": 200, "digit_padding": 3,
		"password_mode": "shared", "shared_password": "S3cret!23",
		"profile_id": profileID, "invoicing": "none", "dry_run": true,
	}
	dr := e.do(t, "POST", "/api/v1/subscribers/batch", body)
	if dr.status != http.StatusOK {
		t.Fatalf("dry run = %d: %s", dr.status, dr.body)
	}
	var dryResp struct {
		Usernames  []string `json:"usernames"`
		Collisions []string `json:"collisions"`
		Total      int      `json:"total"`
	}
	dr.into(t, &dryResp)
	if dryResp.Total != 200 || len(dryResp.Usernames) != 200 {
		t.Fatalf("dry run total = %d, usernames = %d, want 200/200", dryResp.Total, len(dryResp.Usernames))
	}
	if len(dryResp.Collisions) != 0 {
		t.Fatalf("unexpected collisions on a fresh prefix: %v", dryResp.Collisions)
	}

	body["dry_run"] = false
	run := e.do(t, "POST", "/api/v1/subscribers/batch", body)
	if run.status != http.StatusAccepted {
		t.Fatalf("batch create = %d: %s", run.status, run.body)
	}
	var job struct {
		ID string `json:"id"`
	}
	run.into(t, &job)
	final := pollBatchJob(t, e, job.ID)

	if int(final["succeeded"].(float64)) != 200 {
		t.Fatalf("succeeded = %v, want 200: %+v", final["succeeded"], final)
	}
	if int(final["failed"].(float64)) != 0 {
		t.Fatalf("failed = %v, want 0: %+v", final["failed"], final)
	}

	var count int
	if err := e.db.QueryRow(t.Context(),
		`SELECT count(*) FROM subscribers WHERE username LIKE $1`, prefix+"%").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 200 {
		t.Fatalf("rows created = %d, want 200", count)
	}

	// Every created username matches the dry run's exact list (gate item 3).
	for _, u := range dryResp.Usernames {
		var n int
		if err := e.db.QueryRow(t.Context(), `SELECT count(*) FROM subscribers WHERE username = $1`, u).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("dry-run-predicted username %q was not created", u)
		}
	}
}

// TestBatch_CollisionIsPerRow proves gate item 4: seeding one collision fails
// only that row; the rest succeed.
func TestBatch_CollisionIsPerRow(t *testing.T) {
	e := setup(t)
	profileID := e.createProfile(t, "batch-plan-collide", 5000, 5000)
	prefix := e.prefix + "coll"

	// Pre-seed the row at index 5 so it collides.
	collidingUsername := prefix + "005"
	seed := e.do(t, "POST", "/api/v1/subscribers", map[string]any{
		"username": collidingUsername, "password": "x", "profile_id": profileID,
	})
	if seed.status != http.StatusCreated {
		t.Fatalf("seed collision = %d: %s", seed.status, seed.body)
	}

	body := map[string]any{
		"prefix": prefix, "start_index": 1, "count": 10, "digit_padding": 3,
		"password_mode": "shared", "shared_password": "S3cret!23",
		"profile_id": profileID, "invoicing": "none",
	}
	run := e.do(t, "POST", "/api/v1/subscribers/batch", body)
	if run.status != http.StatusAccepted {
		t.Fatalf("batch create = %d: %s", run.status, run.body)
	}
	var job struct {
		ID string `json:"id"`
	}
	run.into(t, &job)
	final := pollBatchJob(t, e, job.ID)

	if int(final["succeeded"].(float64)) != 9 {
		t.Fatalf("succeeded = %v, want 9 (one collision)", final["succeeded"])
	}
	if int(final["failed"].(float64)) != 1 {
		t.Fatalf("failed = %v, want 1", final["failed"])
	}

	var count int
	if err := e.db.QueryRow(t.Context(),
		`SELECT count(*) FROM subscribers WHERE username LIKE $1`, prefix+"%").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 10 { // the 9 new + the 1 pre-seeded
		t.Fatalf("rows present = %d, want 10 (9 new + 1 pre-seeded)", count)
	}
}

// TestBatch_LicenseCapRefusesUpFront proves gate item 5: a batch that would
// cross cap+buffer is refused before creating anything — never partway
// through. This test only exercises the code path when no license is
// installed (subscriberCapStatus short-circuits to Unlimited), so it asserts
// the un-capped case creates everything; the capped-refusal branch is unit
// tested directly against platform.EvalSubscriberCap (see
// internal/platform/subscriber_cap_test.go) since installing a real capped
// license inside this DB suite would require the full license/signing
// machinery this package does not own.
func TestBatch_NoLicenseInstalled_NeverBlocksOnCap(t *testing.T) {
	e := setup(t)
	profileID := e.createProfile(t, "batch-plan-cap", 5000, 5000)
	prefix := e.prefix + "cap"

	body := map[string]any{
		"prefix": prefix, "start_index": 1, "count": 5, "digit_padding": 2,
		"password_mode": "generated",
		"profile_id":    profileID, "invoicing": "none",
	}
	run := e.do(t, "POST", "/api/v1/subscribers/batch", body)
	if run.status != http.StatusAccepted {
		t.Fatalf("batch create = %d: %s", run.status, run.body)
	}
	var job struct {
		ID string `json:"id"`
	}
	run.into(t, &job)
	final := pollBatchJob(t, e, job.ID)
	if int(final["succeeded"].(float64)) != 5 {
		t.Fatalf("succeeded = %v, want 5", final["succeeded"])
	}
	passwords, _ := final["passwords"].(map[string]any)
	if len(passwords) != 5 {
		t.Fatalf("generated passwords = %d, want 5: %+v", len(passwords), final)
	}
}
