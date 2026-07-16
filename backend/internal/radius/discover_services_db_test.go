package radius

// FR-62.6 discover-services endpoint tests (DB-gated, reusing db_phase4_test.go's
// module + fake-router harness). The merge behaviour is the risk here: an
// operator presses "Detect" on a NAS that is already carrying live subscribers
// scoped to its instances, so discovery must never orphan an existing row.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hikrad/hikrad/internal/radius/vendor"
)

func discoverReq(t *testing.T, nasID string) *http.Request {
	t.Helper()
	return autoSetupRequest(t, "POST", "/discover-services", nasID, nil)
}

type discoverResp struct {
	Items []discoveredServiceView `json:"items"`
}

func TestDiscoverServices_NoCredentials_Returns422(t *testing.T) {
	m := autoSetupTestModule(t)
	n := mustInsertNAS(t, m, "", "", "7")

	rec := httptest.NewRecorder()
	m.discoverServicesHandler(rec, discoverReq(t, n.ID))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("discover with no credentials = %d, want 422: %s", rec.Code, rec.Body)
	}
}

func TestDiscoverServices_RouterUnreachable_Returns502(t *testing.T) {
	m := autoSetupTestModule(t)
	m.dialROS = func(context.Context, string, int, string, string) (vendor.ROSConn, error) {
		return nil, errors.New("dial tcp: connection refused")
	}
	n := mustInsertNAS(t, m, "admin", "pass", "7")

	rec := httptest.NewRecorder()
	m.discoverServicesHandler(rec, discoverReq(t, n.ID))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("discover against an unreachable router = %d, want 502: %s", rec.Code, rec.Body)
	}
}

// The headline case: a router running PPPoE + two hotspot zones. mustInsertNAS
// gives the NAS one pppoe service already, so this also covers the merge —
// the existing row must come back matched, not duplicated.
func TestDiscoverServices_ReportsRouterServicesAndMatchesExisting(t *testing.T) {
	m := autoSetupTestModule(t)
	router := newFakeRouter()
	router.rows["/interface/pppoe-server/server/print"] = []map[string]string{
		{"service-name": "test-pppoe", "interface": "ether1", "disabled": "false"},
	}
	router.rows["/ip/hotspot/print"] = []map[string]string{
		{"name": "lobby", "interface": "bridge1", "address-pool": "hs-lobby", "disabled": "false"},
		{"name": "cafe", "interface": "bridge2", "address-pool": "hs-cafe", "disabled": "true"},
	}
	m.dialROS = dialFakeRouter(router)
	n := mustInsertNAS(t, m, "admin", "pass", "7")

	// Name the NAS's existing pppoe service the same as the router's, so the
	// endpoint should report it as already-imported.
	if _, err := m.db.Exec(context.Background(),
		`UPDATE nas_services SET ros_server_name = 'test-pppoe' WHERE nas_id = $1::uuid AND service = 'pppoe'`,
		n.ID); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	m.discoverServicesHandler(rec, discoverReq(t, n.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("discover = %d: %s", rec.Code, rec.Body)
	}
	got := decodeJSON[discoverResp](t, rec)
	if len(got.Items) != 3 {
		t.Fatalf("found %d services, want 3 (1 pppoe + 2 hotspot): %+v", len(got.Items), got.Items)
	}

	byName := map[string]discoveredServiceView{}
	for _, it := range got.Items {
		byName[it.ROSServerName] = it
	}

	// The already-present pppoe instance links to its row, so the panel offers a
	// refresh rather than a duplicate (which would collide on ros_server_name).
	if byName["test-pppoe"].MatchedServiceID == "" {
		t.Errorf("the NAS's existing pppoe service was not matched: %+v", byName["test-pppoe"])
	}
	// The hotspot zones are new to HikRAD.
	if byName["lobby"].MatchedServiceID != "" {
		t.Errorf("lobby is not on this NAS yet but was reported as matched: %+v", byName["lobby"])
	}
	// The router's own pool name comes back verbatim — this is the string an
	// operator otherwise retypes, and mistyping it is the "no address from ip
	// pool" login failure.
	if byName["lobby"].RouterPoolName != "hs-lobby" {
		t.Errorf("lobby pool = %q, want the router's hs-lobby", byName["lobby"].RouterPoolName)
	}
	if byName["cafe"].Enabled {
		t.Errorf("cafe is disabled on the router but came back enabled: %+v", byName["cafe"])
	}
}

// Discovery must not write to HikRAD: it proposes, the operator saves through
// the normal services[] contract. A discovery mistake must never silently
// rewrite a NAS that is authenticating people.
func TestDiscoverServices_WritesNothing(t *testing.T) {
	m := autoSetupTestModule(t)
	router := newFakeRouter()
	router.rows["/ip/hotspot/print"] = []map[string]string{{"name": "lobby"}}
	m.dialROS = dialFakeRouter(router)
	n := mustInsertNAS(t, m, "admin", "pass", "7")

	ctx := context.Background()
	before, err := listServices(ctx, m.db, n.ID)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	m.discoverServicesHandler(rec, discoverReq(t, n.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("discover = %d: %s", rec.Code, rec.Body)
	}

	after, err := listServices(ctx, m.db, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("discovery changed the NAS's services: %d rows before, %d after", len(before), len(after))
	}
	// And nothing was written to the router either (read-only, FR-56.4).
	if len(router.writes) != 0 {
		t.Fatalf("discovery wrote to the router: %+v", router.writes)
	}
}
