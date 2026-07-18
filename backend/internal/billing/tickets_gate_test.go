package billing_test

// Gate items 2-8 for v2-2 (FR-77-80): trial grant/reset, owner scoping,
// wholesale-aware approval, the notification matrix, attachment
// authorization, and the no-account-fallback rule. All run against the
// shared DB-gated env (setup(t), same as db_test.go). Ticket submission goes
// straight through billing.SubmitTicket (the same exported seam portalapi
// calls) rather than a raw HTTP multipart POST, since portalapi's router
// isn't mounted in this package's httptest server; approve/reject/list/
// detail/attachment-retrieval ARE billing's own routes, so those go through
// e.do like every other test in this package.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/billing"
	"github.com/redis/go-redis/v9"
)

func (e env) createProvider(t *testing.T, name string) string {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/payment-providers", e.token,
		map[string]any{"name": name, "instructions_template": "send and note the reference"})
	if r.status != http.StatusCreated {
		t.Fatalf("create provider = %d: %s", r.status, r.body)
	}
	var v struct {
		ID string `json:"id"`
	}
	r.into(t, &v)
	return v.ID
}

func (e env) putProviderAccount(t *testing.T, token, managerID, providerID, details string) {
	t.Helper()
	r := e.do(t, "PUT", "/api/v1/managers/"+managerID+"/provider-accounts/"+providerID, token,
		map[string]any{"account_details": details})
	if r.status != http.StatusOK {
		t.Fatalf("put provider account = %d: %s", r.status, r.body)
	}
}

func (e env) putMethodSetting(t *testing.T, token, managerID, methodKey string, enabled bool) {
	t.Helper()
	r := e.do(t, "PUT", "/api/v1/managers/"+managerID+"/method-settings", token,
		map[string]any{"method_key": methodKey, "enabled": enabled})
	if r.status != http.StatusOK {
		t.Fatalf("put method setting = %d: %s", r.status, r.body)
	}
}

// seedVerifier is a scoped agent granted payment_tickets.verify via a
// per-manager override — the builtin agent role never carries it, so the
// owner-scoping gate item needs its own scoped-but-authorized caller.
func (e env) seedVerifier(t *testing.T) (id, token string) {
	t.Helper()
	id, token = e.seedAgent(t)
	if _, err := e.db.Exec(context.Background(),
		`INSERT INTO manager_permission_overrides (manager_id, permission, granted) VALUES ($1::uuid, 'payment_tickets.verify', true)`,
		id); err != nil {
		t.Fatalf("grant payment_tickets.verify: %v", err)
	}
	// Overrides resolve at login; re-login to pick it up.
	return id, e.login(t, e.usernameFor(t, id), "agentpw")
}

func (e env) usernameFor(t *testing.T, managerID string) string {
	t.Helper()
	var u string
	if err := e.db.QueryRow(context.Background(), `SELECT username FROM managers WHERE id=$1::uuid`, managerID).Scan(&u); err != nil {
		t.Fatal(err)
	}
	return u
}

func (e env) ownedSubscriber(t *testing.T, profileID, ownerID string) string {
	t.Helper()
	sub := e.createSubscriber(t, profileID)
	if _, err := e.db.Exec(context.Background(),
		`UPDATE subscribers SET owner_manager_id=$1::uuid WHERE id=$2::uuid`, ownerID, sub); err != nil {
		t.Fatal(err)
	}
	return sub
}

type ticketListResp struct {
	Items []struct {
		ID             string `json:"id"`
		State          string `json:"state"`
		OwnerManagerID string `json:"owner_manager_id"`
	} `json:"items"`
}

func (e env) listTickets(t *testing.T, token, query string) ticketListResp {
	t.Helper()
	r := e.do(t, "GET", "/api/v1/payment-tickets"+query, token, nil)
	if r.status != http.StatusOK {
		t.Fatalf("list tickets = %d: %s", r.status, r.body)
	}
	var out ticketListResp
	r.into(t, &out)
	return out
}

// --- gate item 2: trial granted on first attempt (AC-78a) ------------------

func TestTrialGrantedOnFirstAttempt(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30)
	agentID, _ := e.seedAgent(t)
	sub := e.ownedSubscriber(t, prof, agentID)
	e.putMethodSetting(t, e.token, agentID, "scratch_card", true)

	before := time.Now().UTC()
	res, err := billing.SubmitTicket(context.Background(), billing.SubmitTicketRequest{
		SubscriberID: sub, MethodKey: "scratch_card", CardType: "zain", CardCode: "1234-5678-9012",
	})
	if err != nil {
		t.Fatalf("submit ticket: %v", err)
	}
	if res.State != "pending" {
		t.Fatalf("state = %q, want pending", res.State)
	}
	if !res.TrialGranted {
		t.Fatal("first attempt did not grant a trial (AC-78a)")
	}
	if res.TrialExpiresAt == nil {
		t.Fatal("trial granted but TrialExpiresAt is nil")
	}
	if res.TrialExpiresAt.Sub(before) < 20*time.Hour || res.TrialExpiresAt.Sub(before) > 28*time.Hour {
		t.Fatalf("trial expiry %v is not ~1 day out from %v", *res.TrialExpiresAt, before)
	}

	var expiresAt time.Time
	if err := e.db.QueryRow(context.Background(), `SELECT expires_at FROM subscribers WHERE id=$1::uuid`, sub).
		Scan(&expiresAt); err != nil {
		t.Fatal(err)
	}
	if expiresAt.Sub(before) < 20*time.Hour {
		t.Fatalf("subscriber expires_at %v not extended by the trial", expiresAt)
	}
}

// --- gate item 3: no trial on post-rejection retry, reset on approval
// (AC-78b) -------------------------------------------------------------------

func TestNoTrialOnRetryResetOnApproval(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30)
	agentID, agentTok := e.seedVerifier(t)
	sub := e.ownedSubscriber(t, prof, agentID)
	e.putMethodSetting(t, e.token, agentID, "scratch_card", true)

	submit := func() billing.TicketSubmitResult {
		res, err := billing.SubmitTicket(context.Background(), billing.SubmitTicketRequest{
			SubscriberID: sub, MethodKey: "scratch_card", CardType: "zain", CardCode: "code-" + time.Now().Format("150405.000000"),
		})
		if err != nil {
			t.Fatalf("submit ticket: %v", err)
		}
		return res
	}

	first := submit()
	if !first.TrialGranted {
		t.Fatal("first submission must grant a trial")
	}

	rr := e.do(t, "POST", "/api/v1/payment-tickets/"+first.ID+"/reject", agentTok, map[string]any{"reason": "bad code"})
	if rr.status != http.StatusOK {
		t.Fatalf("reject = %d: %s", rr.status, rr.body)
	}

	second := submit()
	if second.State != "pending" {
		t.Fatalf("resubmission after rejection state = %q, want pending (must still be accepted)", second.State)
	}
	if second.TrialGranted {
		t.Fatal("resubmission immediately after a rejection must NOT grant a trial (AC-78b)")
	}

	ar := e.do(t, "POST", "/api/v1/payment-tickets/"+second.ID+"/approve", agentTok, nil)
	if ar.status != http.StatusOK {
		t.Fatalf("approve = %d: %s", ar.status, ar.body)
	}

	third := submit()
	if !third.TrialGranted {
		t.Fatal("submission after an approval must grant a trial again — eligibility resets on approval (AC-78b)")
	}
}

// --- gate item 4: owner-scoping + admin-sees-all (AC-79a) -------------------

func TestOwnerScopingAdminSeesAll(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30)
	agentA, agentATok := e.seedVerifier(t)
	agentB, _ := e.seedVerifier(t)
	subA := e.ownedSubscriber(t, prof, agentA)
	subB := e.ownedSubscriber(t, prof, agentB)
	e.putMethodSetting(t, e.token, agentA, "scratch_card", true)
	e.putMethodSetting(t, e.token, agentB, "scratch_card", true)

	if _, err := billing.SubmitTicket(context.Background(), billing.SubmitTicketRequest{
		SubscriberID: subA, MethodKey: "scratch_card", CardType: "zain", CardCode: "a-code",
	}); err != nil {
		t.Fatalf("submit A: %v", err)
	}
	if _, err := billing.SubmitTicket(context.Background(), billing.SubmitTicketRequest{
		SubscriberID: subB, MethodKey: "scratch_card", CardType: "zain", CardCode: "b-code",
	}); err != nil {
		t.Fatalf("submit B: %v", err)
	}

	mine := e.listTickets(t, agentATok, "?scope=mine&state=pending")
	for _, it := range mine.Items {
		if it.OwnerManagerID != agentA {
			t.Fatalf("agent A's scope=mine leaked a ticket owned by %s", it.OwnerManagerID)
		}
	}
	if len(mine.Items) == 0 {
		t.Fatal("agent A's scope=mine returned none — expected their own pending ticket")
	}

	// A scoped caller's scope=all is silently downgraded to mine, never a 403.
	downgraded := e.listTickets(t, agentATok, "?scope=all&state=pending")
	for _, it := range downgraded.Items {
		if it.OwnerManagerID != agentA {
			t.Fatalf("agent A's scope=all was not downgraded — saw a ticket owned by %s", it.OwnerManagerID)
		}
	}

	all := e.listTickets(t, e.token, "?scope=all&state=pending")
	seenA, seenB := false, false
	for _, it := range all.Items {
		if it.OwnerManagerID == agentA {
			seenA = true
		}
		if it.OwnerManagerID == agentB {
			seenB = true
		}
	}
	if !seenA || !seenB {
		t.Fatalf("admin scope=all must see every agent's tickets (seenA=%v seenB=%v)", seenA, seenB)
	}
}

// --- gate item 5: wholesale-aware approval (AC-79b) -------------------------

func TestWholesaleAwareApprovalTicket(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30) // retail 25000
	agentID, agentTok := e.seedVerifier(t)
	sub := e.ownedSubscriber(t, prof, agentID)
	e.putMethodSetting(t, e.token, agentID, "scratch_card", true)
	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token, map[string]any{"amount": 100000, "currency": "IQD"})

	rp := e.do(t, "POST", "/api/v1/reseller-prices", e.token,
		map[string]any{"manager_id": agentID, "profile_id": prof, "price": 20000, "currency": "IQD"})
	if rp.status != http.StatusCreated {
		t.Fatalf("set reseller price = %d: %s", rp.status, rp.body)
	}

	res, err := billing.SubmitTicket(context.Background(), billing.SubmitTicketRequest{
		SubscriberID: sub, MethodKey: "scratch_card", CardType: "zain", CardCode: "wholesale-code",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	ar := e.do(t, "POST", "/api/v1/payment-tickets/"+res.ID+"/approve", agentTok, nil)
	if ar.status != http.StatusOK {
		t.Fatalf("approve = %d: %s", ar.status, ar.body)
	}

	if got := e.balanceFor(t, agentID, "IQD"); got != 100000-20000 {
		t.Errorf("agent balance = %d, want %d (debited resolved wholesale 20000, not retail 25000)", got, 100000-20000)
	}
	var receiptAmount int64
	if err := e.db.QueryRow(context.Background(),
		`SELECT amount FROM payments WHERE subscriber_id=$1::uuid ORDER BY at DESC LIMIT 1`, sub).Scan(&receiptAmount); err != nil {
		t.Fatal(err)
	}
	if receiptAmount != 25000 {
		t.Errorf("subscriber receipt = %d, want 25000 (retail, unaffected by the reseller's wholesale price)", receiptAmount)
	}
}

// --- gate item 6: both-sides notifications (AC-80a) -------------------------

func TestBothSidesTicketNotifications(t *testing.T) {
	e := setup(t)
	redisURL := os.Getenv("HIKRAD_TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("HIKRAD_TEST_REDIS_URL not set; skipping notification gate test")
	}
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	rdb := redis.NewClient(opt)
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sub := rdb.Subscribe(ctx, "billing.payment_ticket")
	defer sub.Close()
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	msgs := sub.Channel()

	prof := e.createProfile(t, 25000, 30)
	agentID, _ := e.seedVerifier(t)
	subID := e.ownedSubscriber(t, prof, agentID)
	e.putMethodSetting(t, e.token, agentID, "scratch_card", true)

	res, err := billing.SubmitTicket(context.Background(), billing.SubmitTicketRequest{
		SubscriberID: subID, MethodKey: "scratch_card", CardType: "zain", CardCode: "notify-code",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	type evt struct {
		SubscriberID   string `json:"subscriber_id"`
		TicketID       string `json:"ticket_id"`
		State          string `json:"state"`
		OwnerManagerID string `json:"owner_manager_id"`
		DecidedBy      string `json:"decided_by"`
	}
	next := func() evt {
		select {
		case m := <-msgs:
			var e evt
			if err := json.Unmarshal([]byte(m.Payload), &e); err != nil {
				t.Fatalf("bad payload %s: %v", m.Payload, err)
			}
			return e
		case <-ctx.Done():
			t.Fatal("timed out waiting for billing.payment_ticket")
			return evt{}
		}
	}

	submitted := next()
	if submitted.State != "submitted" || submitted.TicketID != res.ID || submitted.SubscriberID != subID {
		t.Fatalf("submitted event = %+v, want ticket %s / subscriber %s", submitted, res.ID, subID)
	}
	if submitted.OwnerManagerID != agentID {
		t.Fatalf("submitted event owner_manager_id = %q, want %q (so the owning manager can be notified)", submitted.OwnerManagerID, agentID)
	}

	ar := e.do(t, "POST", "/api/v1/payment-tickets/"+res.ID+"/approve", e.token, nil) // decided by admin, not the owner
	if ar.status != http.StatusOK {
		t.Fatalf("approve = %d: %s", ar.status, ar.body)
	}
	approved := next()
	if approved.State != "approved" || approved.TicketID != res.ID {
		t.Fatalf("approved event = %+v", approved)
	}
	if approved.DecidedBy != e.adminID {
		t.Fatalf("approved event decided_by = %q, want admin %q", approved.DecidedBy, e.adminID)
	}
	if approved.DecidedBy == approved.OwnerManagerID {
		t.Fatal("test setup invalid: decider must differ from owner to exercise the 'decided by someone else' branch")
	}

	// Every notification traces to a real payment_ticket_events row (FR-80.3).
	var n int
	if err := e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM payment_ticket_events WHERE ticket_id=$1::uuid AND event_type IN ('submitted','approved')`,
		res.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("payment_ticket_events rows = %d, want 2 (submitted+approved)", n)
	}
}

// --- gate item 7: attachment authorization ----------------------------------

// tinyPNG is just enough of a real PNG signature for net/http's content-type
// sniffer to say image/png — ticket_attachments.go re-validates real bytes,
// never the client-declared header, so this must be a real magic-byte match.
var tinyPNG = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 0, 0, 0, 0}

func TestAttachmentAuthorization(t *testing.T) {
	e := setup(t)
	t.Cleanup(func() { _ = os.RemoveAll("data/payment-attachments") })

	prof := e.createProfile(t, 25000, 30)
	owner, ownerTok := e.seedVerifier(t)
	_, strangerTok := e.seedVerifier(t)
	sub := e.ownedSubscriber(t, prof, owner)
	e.putMethodSetting(t, e.token, owner, "scratch_card", true)

	res, err := billing.SubmitTicket(context.Background(), billing.SubmitTicketRequest{
		SubscriberID: sub, MethodKey: "scratch_card", CardType: "zain", CardCode: "attach-code",
		Attachments: []billing.UploadedFile{{Filename: "proof.png", ContentType: "image/png", Data: tinyPNG}},
	})
	if err != nil {
		t.Fatalf("submit with attachment: %v", err)
	}

	var attachmentID string
	if err := e.db.QueryRow(context.Background(),
		`SELECT id::text FROM payment_ticket_attachments WHERE ticket_id=$1::uuid`, res.ID).Scan(&attachmentID); err != nil {
		t.Fatalf("attachment row missing: %v", err)
	}

	path := "/api/v1/payment-tickets/" + res.ID + "/attachments/" + attachmentID

	if r := e.do(t, "GET", path, ownerTok, nil); r.status != http.StatusOK {
		t.Errorf("owner fetch = %d, want 200", r.status)
	}
	if r := e.do(t, "GET", path, e.token, nil); r.status != http.StatusOK {
		t.Errorf("admin fetch = %d, want 200", r.status)
	}
	if r := e.do(t, "GET", path, strangerTok, nil); r.status != http.StatusForbidden {
		t.Errorf("a different scoped manager's fetch = %d, want 403", r.status)
	}

	// The response always carries Content-Disposition: attachment (C10) — an
	// uploaded file is data, never inline-rendered, regardless of type.
	req, _ := http.NewRequest("GET", e.srv.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+ownerTok)
	hr, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("attachment fetch: %v", err)
	}
	defer hr.Body.Close()
	if disp := hr.Header.Get("Content-Disposition"); disp != `attachment; filename="proof.png"` {
		t.Errorf("Content-Disposition = %q, want attachment; filename=%q", disp, "proof.png")
	}
}

// --- gate item 8: no-account fallback never leaks a method (AC-77a) --------

func TestNoAccountFallbackNeverLeaksMethod(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30)
	agentID, _ := e.seedAgent(t)
	sub := e.ownedSubscriber(t, prof, agentID)
	providerID := e.createProvider(t, "Leaky Wallet")
	// Enabled, but NO manager_provider_accounts row for this manager+provider.
	e.putMethodSetting(t, e.token, agentID, providerID, true)

	methods, err := billing.ResolvePayMethods(context.Background(), sub)
	if err != nil {
		t.Fatalf("resolve pay methods: %v", err)
	}
	for _, m := range methods {
		if m.Key == providerID {
			t.Fatalf("provider %s appeared in the resolved method list despite no configured account (kickoff blocker 1 / AC-77a)", providerID)
		}
	}

	// Configuring the account makes it appear — proves the absence above was
	// the account gate, not some other reason the provider never shows.
	e.putProviderAccount(t, e.token, agentID, providerID, "0770-000-0000")
	methods2, err := billing.ResolvePayMethods(context.Background(), sub)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range methods2 {
		if m.Key == providerID {
			found = true
		}
	}
	if !found {
		t.Fatal("provider still missing after an account was configured — test setup or resolution is broken")
	}
}
