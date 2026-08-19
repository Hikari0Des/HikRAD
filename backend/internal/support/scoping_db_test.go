package support

// Gate item 10: "an agent sees only their own subscribers' tickets" — the
// exact reused auth.ScopeFilter join, proven end to end against a real DB.
// ticketFilters/listTickets take the scope as an explicit parameter (the
// same shape auth.ScopeFilter(ctx) returns), so this test exercises the
// real filtering SQL without needing a full HTTP+login harness — just two
// managers, two owned subscribers, two tickets, and the exact query the
// HTTP handlers run.

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping support DB suite")
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := platform.Migrate(dbURL, "../../migrations", log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := platform.NewDB(context.Background(), platform.Config{DBURL: dbURL})
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

func seedManager(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var id string
	err := db.QueryRow(context.Background(),
		`INSERT INTO managers (username, password_hash, role) VALUES ($1, 'x', 'agent') RETURNING id::text`,
		fmt.Sprintf("agent-%d", time.Now().UnixNano())).Scan(&id)
	if err != nil {
		t.Fatalf("seed manager: %v", err)
	}
	return id
}

func seedOwnedSubscriber(t *testing.T, db *pgxpool.Pool, ownerID string) string {
	t.Helper()
	var id string
	err := db.QueryRow(context.Background(),
		`INSERT INTO subscribers (username, owner_manager_id) VALUES ($1, $2::uuid) RETURNING id::text`,
		fmt.Sprintf("sub-%d", time.Now().UnixNano()), ownerID).Scan(&id)
	if err != nil {
		t.Fatalf("seed subscriber: %v", err)
	}
	return id
}

func TestScoping_AgentSeesOnlyOwnSubscribersTickets(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	agentA := seedManager(t, db)
	agentB := seedManager(t, db)
	subA := seedOwnedSubscriber(t, db, agentA)
	subB := seedOwnedSubscriber(t, db, agentB)

	ticketA, err := createTicket(ctx, db, subA, "A's issue", PriorityNormal, "manager", agentA)
	if err != nil {
		t.Fatalf("create ticket A: %v", err)
	}
	ticketB, err := createTicket(ctx, db, subB, "B's issue", PriorityNormal, "manager", agentB)
	if err != nil {
		t.Fatalf("create ticket B: %v", err)
	}

	// Build a fake request the way listTicketsHandler would receive one —
	// ticketFilters reads only r.URL.Query(), no auth context needed.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/support-tickets", nil)

	scopeA := &auth.ManagerScope{ManagerID: agentA}
	whereA, argsA := ticketFilters(req, scopeA, agentA, 1)
	itemsA, _, err := listTickets(ctx, db, whereA, argsA, nil, 50)
	if err != nil {
		t.Fatalf("list scoped to agent A: %v", err)
	}
	seenA, seenB := false, false
	for _, it := range itemsA {
		if it.ID == ticketA.ID {
			seenA = true
		}
		if it.ID == ticketB.ID {
			seenB = true
		}
	}
	if !seenA {
		t.Fatal("agent A's scoped list is missing their own ticket")
	}
	if seenB {
		t.Fatal("agent A's scoped list leaked agent B's ticket — scoping is broken")
	}

	// An unscoped (admin) caller sees both.
	whereAll, argsAll := ticketFilters(req, nil, "", 1)
	itemsAll, _, err := listTickets(ctx, db, whereAll, argsAll, nil, 50)
	if err != nil {
		t.Fatalf("list unscoped: %v", err)
	}
	seenA, seenB = false, false
	for _, it := range itemsAll {
		if it.ID == ticketA.ID {
			seenA = true
		}
		if it.ID == ticketB.ID {
			seenB = true
		}
	}
	if !seenA || !seenB {
		t.Fatalf("unscoped (admin) caller must see every agent's tickets (seenA=%v seenB=%v)", seenA, seenB)
	}
}

// TestTicketLifecycle_StatusesAndWithdrawalHidesByDefault is gate item 8.
func TestTicketLifecycle_StatusesAndWithdrawalHidesByDefault(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	agent := seedManager(t, db)
	sub := seedOwnedSubscriber(t, db, agent)

	tk, err := createTicket(ctx, db, sub, "lifecycle test", PriorityCritical, "manager", agent)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tk.Priority != PriorityCritical {
		t.Fatalf("priority = %q, want critical", tk.Priority)
	}
	if tk.Status != StatusOpen {
		t.Fatalf("initial status = %q, want open", tk.Status)
	}

	for _, s := range []string{StatusInProgress, StatusResolved, StatusClosed} {
		got, err := setStatus(ctx, db, tk.ID, s)
		if err != nil {
			t.Fatalf("set status %s: %v", s, err)
		}
		if got.Status != s {
			t.Fatalf("status = %q, want %q", got.Status, s)
		}
	}

	// Reopen so withdrawal (only valid from open/in_progress) has something to act on.
	if _, err := setStatus(ctx, db, tk.ID, StatusOpen); err != nil {
		t.Fatal(err)
	}
	if err := withdraw(ctx, db, tk.ID, sub); err != nil {
		t.Fatalf("withdraw: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/support-tickets", nil)
	where, args := ticketFilters(req, nil, "", 1) // default: withdrawn hidden
	items, _, err := listTickets(ctx, db, where, args, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.ID == tk.ID {
			t.Fatal("withdrawn ticket appeared in the default (non-withdrawn) list")
		}
	}

	reqAll := httptest.NewRequest(http.MethodGet, "/api/v1/support-tickets?include_withdrawn=true", nil)
	whereAll, argsAll := ticketFilters(reqAll, nil, "", 1)
	itemsAll, _, err := listTickets(ctx, db, whereAll, argsAll, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range itemsAll {
		if it.ID == tk.ID && it.Status == StatusWithdrawn {
			found = true
		}
	}
	if !found {
		t.Fatal("include_withdrawn=true must still surface the withdrawn ticket")
	}
}
