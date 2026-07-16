package subscribers_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestCRUDValidationMatrix(t *testing.T) {
	e := setup(t)
	profID := e.createProfile(t, uniq("Basic_"), 10240, 10240)
	user := uniq("u_")

	// Create — valid, with phone in local form (normalized on write).
	r := e.do(t, "POST", "/api/v1/subscribers", map[string]any{
		"username": user, "password": "secret1", "name": "علي حسن",
		"phone": "07701234567", "profile_id": profID,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create = %d: %s", r.status, r.body)
	}
	var created struct {
		ID    string  `json:"id"`
		Phone *string `json:"phone"`
	}
	r.into(t, &created)
	if created.Phone == nil || *created.Phone != "+9647701234567" {
		t.Errorf("phone not normalized: %v", created.Phone)
	}
	// A create audit row exists (FR-1.4 / C2).
	if e.auditCount(t, "subscriber.create", created.ID) != 1 {
		t.Errorf("expected one subscriber.create audit row")
	}

	// Create — missing password → 422.
	if r := e.do(t, "POST", "/api/v1/subscribers", map[string]any{"username": uniq("u_")}); r.status != http.StatusUnprocessableEntity {
		t.Errorf("missing password = %d, want 422: %s", r.status, r.body)
	}
	// Create — duplicate username → 409.
	if r := e.do(t, "POST", "/api/v1/subscribers", map[string]any{"username": user, "password": "x"}); r.status != http.StatusConflict {
		t.Errorf("dup username = %d, want 409: %s", r.status, r.body)
	}
	// Create — invalid phone → 422.
	if r := e.do(t, "POST", "/api/v1/subscribers", map[string]any{"username": uniq("u_"), "password": "x", "phone": "12345"}); r.status != http.StatusUnprocessableEntity {
		t.Errorf("bad phone = %d, want 422: %s", r.status, r.body)
	}

	// Update — username immutable → 422.
	if r := e.do(t, "PUT", "/api/v1/subscribers/"+created.ID, map[string]any{"username": "different"}); r.status != http.StatusUnprocessableEntity {
		t.Errorf("username change = %d, want 422: %s", r.status, r.body)
	}

	// Detail composition endpoint returns profile summary + live flag.
	r = e.do(t, "GET", "/api/v1/subscribers/"+created.ID, nil)
	if r.status != http.StatusOK {
		t.Fatalf("detail = %d: %s", r.status, r.body)
	}
	var detail struct {
		Subscriber struct {
			ID string `json:"id"`
		} `json:"subscriber"`
		Profile *struct {
			ID string `json:"id"`
		} `json:"profile"`
		Live struct {
			Online bool `json:"online"`
		} `json:"live"`
		Links map[string]string `json:"links"`
	}
	r.into(t, &detail)
	if detail.Subscriber.ID != created.ID || detail.Profile == nil || detail.Profile.ID != profID {
		t.Errorf("detail composition wrong: %s", r.body)
	}
	if detail.Links["usage"] == "" {
		t.Errorf("detail missing usage link")
	}

	// Delete → 204, then gone.
	if r := e.do(t, "DELETE", "/api/v1/subscribers/"+created.ID, nil); r.status != http.StatusNoContent {
		t.Fatalf("delete = %d: %s", r.status, r.body)
	}
	if r := e.do(t, "GET", "/api/v1/subscribers/"+created.ID, nil); r.status != http.StatusNotFound {
		t.Errorf("get after delete = %d, want 404", r.status)
	}
	if e.auditCount(t, "subscriber.delete", created.ID) != 1 {
		t.Errorf("expected one subscriber.delete audit row")
	}
}

func TestWhatsappOptInRequiresPhone(t *testing.T) {
	e := setup(t)
	// opt-in with no phone → 422.
	if r := e.do(t, "POST", "/api/v1/subscribers", map[string]any{
		"username": uniq("wa_"), "password": "x", "whatsapp_opt_in": true,
	}); r.status != http.StatusUnprocessableEntity {
		t.Errorf("opt-in without phone = %d, want 422: %s", r.status, r.body)
	}
	// opt-in with a valid phone → created.
	if r := e.do(t, "POST", "/api/v1/subscribers", map[string]any{
		"username": uniq("wa_"), "password": "x", "phone": "07711112222", "whatsapp_opt_in": true,
	}); r.status != http.StatusCreated {
		t.Errorf("opt-in with phone = %d, want 201: %s", r.status, r.body)
	}
}

func TestSearchLatency5k(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	// Seed 5 000 rows in one statement, with a run-unique username prefix and an
	// Arabic name fragment to exercise the folded index.
	tag := e.prefix
	if _, err := e.db.Exec(ctx,
		`INSERT INTO subscribers (username, password_enc, name, phone, status)
		 SELECT $1||'-'||g, '\x01'::bytea, 'مشترك رقم '||g, '+9647'||lpad(g::text,9,'0'), 'active'
		 FROM generate_series(1,5000) g`, tag); err != nil {
		t.Fatal(err)
	}
	// Clean up the bulk rows so they don't skew other suites' whole-table
	// assertions on the shared DB.
	t.Cleanup(func() {
		_, _ = e.db.Exec(context.Background(), `DELETE FROM subscribers WHERE username LIKE $1||'-%'`, tag)
	})

	// Prefix fragment match must return < 300 ms (AC-2a).
	start := time.Now()
	r := e.do(t, "GET", "/api/v1/search?q="+tag+"-4242", nil)
	elapsed := time.Since(start)
	if r.status != http.StatusOK {
		t.Fatalf("search = %d: %s", r.status, r.body)
	}
	var out struct {
		Items []struct {
			Username string `json:"username"`
		} `json:"items"`
	}
	r.into(t, &out)
	if len(out.Items) == 0 {
		t.Errorf("search returned no hits for %s-4242", tag)
	}
	if elapsed > 300*time.Millisecond {
		t.Errorf("search took %v, want < 300ms", elapsed)
	}
	t.Logf("search over 5k rows took %v", elapsed)
}

func TestBulkAsyncWithFailures(t *testing.T) {
	e := setup(t)
	profID := e.createProfile(t, uniq("Bulk_"), 10240, 10240)

	// Create three subscribers under the run tag.
	tag := uniq("bulk_")
	var ids []string
	for i := 0; i < 3; i++ {
		r := e.do(t, "POST", "/api/v1/subscribers", map[string]any{
			"username": fmt.Sprintf("%s-%d", tag, i), "password": "x", "profile_id": profID,
			"expires_at": time.Now().UTC().Format(time.RFC3339),
		})
		if r.status != http.StatusCreated {
			t.Fatalf("seed create = %d: %s", r.status, r.body)
		}
		var c struct {
			ID string `json:"id"`
		}
		r.into(t, &c)
		ids = append(ids, c.ID)
	}

	// Happy path: extend expiry +7 days for the tag → all succeed.
	r := e.do(t, "POST", "/api/v1/subscribers/bulk", map[string]any{
		"filter": map[string]any{"q": tag},
		"action": "extend_expiry",
		"params": map[string]any{"days": 7},
	})
	if r.status != http.StatusAccepted {
		t.Fatalf("bulk = %d: %s", r.status, r.body)
	}
	var job struct {
		ID    string `json:"id"`
		Total int    `json:"total"`
	}
	r.into(t, &job)
	if job.Total != 3 {
		t.Errorf("job total = %d, want 3", job.Total)
	}
	final := e.pollJob(t, job.ID)
	if final.Succeeded != 3 || final.Failed != 0 {
		t.Errorf("extend job: succeeded=%d failed=%d", final.Succeeded, final.Failed)
	}
	// One audit row per affected subscriber (AC-4a).
	if n := e.auditCount(t, "subscriber.bulk_extend_expiry", ids[0]); n != 1 {
		t.Errorf("expected one bulk_extend_expiry audit for %s, got %d", ids[0], n)
	}

	// Induced per-row failure: move_owner to a non-existent manager id → each row
	// fails the FK, reported per-row.
	r = e.do(t, "POST", "/api/v1/subscribers/bulk", map[string]any{
		"filter": map[string]any{"q": tag},
		"action": "move_owner",
		"params": map[string]any{"owner_manager_id": "00000000-0000-0000-0000-000000000000"},
	})
	if r.status != http.StatusAccepted {
		t.Fatalf("bulk move = %d: %s", r.status, r.body)
	}
	r.into(t, &job)
	final = e.pollJob(t, job.ID)
	if final.Failed != 3 || len(final.Failures) != 3 {
		t.Errorf("move job: expected 3 per-row failures, got failed=%d failures=%d", final.Failed, len(final.Failures))
	}
}

func TestBulkExportGatedAndCSV(t *testing.T) {
	e := setup(t)
	tag := uniq("exp_")
	for i := 0; i < 2; i++ {
		if r := e.do(t, "POST", "/api/v1/subscribers", map[string]any{
			"username": fmt.Sprintf("%s-%d", tag, i), "password": "x",
		}); r.status != http.StatusCreated {
			t.Fatalf("seed = %d: %s", r.status, r.body)
		}
	}
	r := e.do(t, "POST", "/api/v1/subscribers/bulk", map[string]any{
		"filter": map[string]any{"q": tag}, "action": "export",
	})
	if r.status != http.StatusOK {
		t.Fatalf("export = %d: %s", r.status, r.body)
	}
	// CSV header + 2 rows.
	if got := string(r.body); !contains(got, "username,name,phone,status") {
		t.Errorf("csv missing header: %s", got)
	}
}

func TestProfileArchiveRules(t *testing.T) {
	e := setup(t)
	active := e.createProfile(t, uniq("Act_"), 10240, 10240)
	archived := e.createProfile(t, uniq("Arc_"), 10240, 10240)

	// Archive one profile.
	if r := e.do(t, "POST", "/api/v1/profiles/"+archived+"/archive", nil); r.status != http.StatusOK {
		t.Fatalf("archive = %d: %s", r.status, r.body)
	}
	// A subscriber exists to be bulk-changed.
	tag := uniq("arc_")
	if r := e.do(t, "POST", "/api/v1/subscribers", map[string]any{
		"username": tag, "password": "x", "profile_id": active,
	}); r.status != http.StatusCreated {
		t.Fatalf("seed = %d: %s", r.status, r.body)
	}
	// Bulk change to the archived target must be rejected up front.
	if r := e.do(t, "POST", "/api/v1/subscribers/bulk", map[string]any{
		"filter": map[string]any{"q": tag},
		"action": "change_profile",
		"params": map[string]any{"profile_id": archived},
	}); r.status != http.StatusUnprocessableEntity {
		t.Errorf("bulk change to archived = %d, want 422: %s", r.status, r.body)
	}
	// Archived profiles are hidden from the default list, shown with ?archived=true.
	r := e.do(t, "GET", "/api/v1/profiles", nil)
	if containsID(r.body, archived) {
		t.Errorf("archived profile leaked into default list")
	}
	r = e.do(t, "GET", "/api/v1/profiles?archived=true", nil)
	if !containsID(r.body, archived) {
		t.Errorf("archived profile missing from ?archived=true list")
	}
}

func TestAuthViewInvalidatedOnMutation(t *testing.T) {
	e := setup(t)
	if e.rdb == nil {
		t.Skip("HIKRAD_TEST_REDIS_URL not set; skipping cache-invalidation test")
	}
	ctx := context.Background()
	profID := e.createProfile(t, uniq("Inv_"), 10240, 10240)
	user := uniq("inv_")
	r := e.do(t, "POST", "/api/v1/subscribers", map[string]any{
		"username": user, "password": "x", "profile_id": profID,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create = %d: %s", r.status, r.body)
	}
	var c struct {
		ID string `json:"id"`
	}
	r.into(t, &c)

	// Simulate B's engine having cached this subscriber's view.
	seedCache := func() {
		e.rdb.Set(ctx, "auth:view:"+user, `{"subscriber_id":"`+c.ID+`"}`, time.Minute)
		e.rdb.Set(ctx, "auth:view:sub:"+c.ID, user, time.Minute)
	}
	cached := func() bool {
		n, _ := e.rdb.Exists(ctx, "auth:view:"+user).Result()
		return n == 1
	}

	for _, tc := range []struct {
		name string
		fn   func()
	}{
		{"update", func() { e.do(t, "PUT", "/api/v1/subscribers/"+c.ID, map[string]any{"name": "New"}) }},
		{"reset-mac", func() { e.do(t, "POST", "/api/v1/subscribers/"+c.ID+"/reset-mac", nil) }},
	} {
		seedCache()
		if !cached() {
			t.Fatalf("%s: precondition — cache not seeded", tc.name)
		}
		tc.fn()
		if cached() {
			t.Errorf("%s: cached view not invalidated", tc.name)
		}
	}
}

// --- helpers ---------------------------------------------------------------

type jobResult struct {
	Status    string `json:"status"`
	Succeeded int    `json:"succeeded"`
	Failed    int    `json:"failed"`
	Failures  []struct {
		Error string `json:"error"`
	} `json:"failures"`
}

// getSubscriberStatus reads one subscriber's status through the API.
func (e testEnv) getSubscriberStatus(t *testing.T, id string) string {
	t.Helper()
	r := e.do(t, "GET", "/api/v1/subscribers/"+id, nil)
	if r.status != http.StatusOK {
		t.Fatalf("get %s = %d: %s", id, r.status, r.body)
	}
	var s struct {
		Subscriber struct {
			Status string `json:"status"`
		} `json:"subscriber"`
	}
	r.into(t, &s)
	return s.Subscriber.Status
}

func (e testEnv) pollJob(t *testing.T, id string) jobResult {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		r := e.do(t, "GET", "/api/v1/subscribers/bulk/"+id, nil)
		if r.status != http.StatusOK {
			t.Fatalf("job status = %d: %s", r.status, r.body)
		}
		var jr jobResult
		r.into(t, &jr)
		if jr.Status == "completed" {
			return jr
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("job %s did not complete in time", id)
	return jobResult{}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func containsID(body []byte, id string) bool { return indexOf(string(body), id) >= 0 }

// --- selection-based bulk + delete (owner-requested 2026-07-16) ------------

// A selection means THOSE rows, and nothing else. Filter-only bulk could not
// express "these two of the three", so an operator either edited rows one at a
// time or built a filter and hoped it matched nothing extra.
func TestBulkBySelectionIgnoresFilter(t *testing.T) {
	e := setup(t)
	profID := e.createProfile(t, uniq("Sel_"), 10240, 10240)
	tag := uniq("sel_")
	var ids []string
	for i := 0; i < 3; i++ {
		r := e.do(t, "POST", "/api/v1/subscribers", map[string]any{
			"username": fmt.Sprintf("%s-%d", tag, i), "password": "x", "profile_id": profID,
		})
		if r.status != http.StatusCreated {
			t.Fatalf("seed create = %d: %s", r.status, r.body)
		}
		var c struct {
			ID string `json:"id"`
		}
		r.into(t, &c)
		ids = append(ids, c.ID)
	}

	// Two of the three, and a filter that would have matched all three — the
	// selection must win, not union or intersect.
	r := e.do(t, "POST", "/api/v1/subscribers/bulk", map[string]any{
		"filter":         map[string]any{"q": tag},
		"subscriber_ids": []string{ids[0], ids[2]},
		"action":         "disable",
	})
	if r.status != http.StatusAccepted {
		t.Fatalf("bulk = %d: %s", r.status, r.body)
	}
	var job struct {
		ID    string `json:"id"`
		Total int    `json:"total"`
	}
	r.into(t, &job)
	if job.Total != 2 {
		t.Fatalf("job total = %d, want exactly the 2 selected rows", job.Total)
	}
	final := e.pollJob(t, job.ID)
	if final.Succeeded != 2 {
		t.Fatalf("succeeded=%d failed=%d, want 2 succeeded", final.Succeeded, final.Failed)
	}
	// The unselected one is untouched, which is the whole point.
	if got := e.getSubscriberStatus(t, ids[1]); got != "active" {
		t.Errorf("the unselected subscriber's status = %q, want it untouched (active)", got)
	}
}

// Bulk delete removes the rows an operator selected.
func TestBulkDeleteBySelection(t *testing.T) {
	e := setup(t)
	tag := uniq("del_")
	r := e.do(t, "POST", "/api/v1/subscribers", map[string]any{"username": tag, "password": "x"})
	if r.status != http.StatusCreated {
		t.Fatalf("seed create = %d: %s", r.status, r.body)
	}
	var c struct {
		ID string `json:"id"`
	}
	r.into(t, &c)

	r = e.do(t, "POST", "/api/v1/subscribers/bulk", map[string]any{
		"subscriber_ids": []string{c.ID},
		"action":         "delete",
	})
	if r.status != http.StatusAccepted {
		t.Fatalf("bulk delete = %d: %s", r.status, r.body)
	}
	var job struct {
		ID string `json:"id"`
	}
	r.into(t, &job)
	final := e.pollJob(t, job.ID)
	if final.Succeeded != 1 || final.Failed != 0 {
		t.Fatalf("delete job: succeeded=%d failed=%d, want 1/0 (%+v)", final.Succeeded, final.Failed, final.Failures)
	}
	if got := e.do(t, "GET", "/api/v1/subscribers/"+c.ID, nil); got.status != http.StatusNotFound {
		t.Errorf("GET after bulk delete = %d, want 404", got.status)
	}
}

// An unknown action is still rejected — `delete` joining the allowlist must not
// have opened it up.
func TestBulkUnknownActionStillRejected(t *testing.T) {
	e := setup(t)
	r := e.do(t, "POST", "/api/v1/subscribers/bulk", map[string]any{
		"subscriber_ids": []string{"00000000-0000-0000-0000-000000000000"},
		"action":         "drop_database",
	})
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("unknown action = %d, want 422: %s", r.status, r.body)
	}
}
