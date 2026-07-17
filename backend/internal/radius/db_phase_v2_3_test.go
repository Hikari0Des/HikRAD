package radius

// DB-backed suite for v2 phase 3 (FR-65/66/67), gated on HIKRAD_TEST_DB_URL
// like db_phase4_test.go, whose helpers (autoSetupTestModule, mustInsertNAS,
// fakeRouter, dialFakeRouter, autoSetupRequest, decodeJSON) this file reuses.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// serviceRequest is autoSetupRequest with a second URL param (serviceId) —
// the FR-67 endpoints are scoped by both nas id and service id.
func serviceRequest(t *testing.T, method, path, nasID, serviceID string, body any) *http.Request {
	t.Helper()
	req := autoSetupRequest(t, method, path, nasID, body)
	rctx := chi.RouteContext(req.Context())
	rctx.URLParams.Add("serviceId", serviceID)
	return req
}

func firstServiceID(t *testing.T, m *module, nasID string) string {
	t.Helper()
	var id string
	if err := m.db.QueryRow(context.Background(),
		`SELECT id::text FROM nas_services WHERE nas_id = $1::uuid ORDER BY created_at LIMIT 1`, nasID).Scan(&id); err != nil {
		t.Fatalf("lookup seeded service: %v", err)
	}
	return id
}

func serviceManagementMode(t *testing.T, m *module, serviceID string) string {
	t.Helper()
	var mode string
	if err := m.db.QueryRow(context.Background(),
		`SELECT management_mode FROM nas_services WHERE id = $1::uuid`, serviceID).Scan(&mode); err != nil {
		t.Fatalf("lookup service management_mode: %v", err)
	}
	return mode
}

// --- FR-65: config inspection ------------------------------------------

func TestNASConfig_NoCredentials_Returns422(t *testing.T) {
	m := autoSetupTestModule(t)
	n := mustInsertNAS(t, m, "", "", "7")

	rec := httptest.NewRecorder()
	m.nasConfigHandler(rec, autoSetupRequest(t, "GET", "/config", n.ID, nil))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("config with no credentials = %d, want 422: %s", rec.Code, rec.Body)
	}
}

func TestNASConfig_HappyPath_ReturnsSnapshotAndAudits(t *testing.T) {
	m := autoSetupTestModule(t)
	router := newFakeRouter()
	router.rows["/radius/print"] = []map[string]string{{"address": "10.0.0.5", "service": "ppp", "secret": "x", "comment": "hikrad-auto"}}
	m.dialROS = dialFakeRouter(router)
	n := mustInsertNAS(t, m, "admin", "pass", "7")

	rec := httptest.NewRecorder()
	m.nasConfigHandler(rec, autoSetupRequest(t, "GET", "/config", n.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("config = %d: %s", rec.Code, rec.Body)
	}
	resp := decodeJSON[nasConfigResponse](t, rec)
	if len(resp.Radius) != 1 || resp.Radius[0].Address != "10.0.0.5" {
		t.Fatalf("expected the planted /radius entry to be reflected, got %+v", resp.Radius)
	}
}

// --- FR-67: server management -------------------------------------------

func TestServiceApply_CreateHotspot_PersistsSystemManaged(t *testing.T) {
	m := autoSetupTestModule(t)
	router := newFakeRouter()
	m.dialROS = dialFakeRouter(router)
	n := mustInsertNAS(t, m, "admin", "pass", "7")

	body := map[string]any{
		"service": "hotspot", "ros_server_name": "zone2", "label": "Zone 2", "interface": "bridge-zone2",
	}
	planRec := httptest.NewRecorder()
	m.servicePlanHandler(planRec, autoSetupRequest(t, "POST", "/services/plan", n.ID, body))
	if planRec.Code != http.StatusOK {
		t.Fatalf("plan = %d: %s", planRec.Code, planRec.Body)
	}
	preview := decodeJSON[autoSetupPreviewResponse](t, planRec)
	if len(preview.Conflicts) != 0 || len(preview.Items) == 0 {
		t.Fatalf("expected a clean, non-empty create plan, got %+v", preview)
	}

	applyBody := map[string]any{
		"service": "hotspot", "ros_server_name": "zone2", "label": "Zone 2", "interface": "bridge-zone2",
		"preview_hash": preview.PreviewHash,
	}
	applyRec := httptest.NewRecorder()
	m.serviceApplyHandler(applyRec, autoSetupRequest(t, "POST", "/services/apply", n.ID, applyBody))
	if applyRec.Code != http.StatusOK {
		t.Fatalf("apply = %d: %s", applyRec.Code, applyRec.Body)
	}
	applied := decodeJSON[serviceApplyResponse](t, applyRec)
	if !applied.AllOK {
		t.Fatalf("expected a clean apply, got %+v", applied.Results)
	}
	if applied.Service.ManagementMode != "system" {
		t.Fatalf("expected the newly created service to be management_mode=system, got %q", applied.Service.ManagementMode)
	}
	if got := serviceManagementMode(t, m, applied.Service.ID); got != "system" {
		t.Fatalf("expected the persisted row's management_mode = system, got %q", got)
	}
}

func TestServiceEdit_RouterManaged_RequiresAdopt(t *testing.T) {
	m := autoSetupTestModule(t)
	router := newFakeRouter()
	m.dialROS = dialFakeRouter(router)
	n := mustInsertNAS(t, m, "admin", "pass", "7")
	serviceID := firstServiceID(t, m, n.ID) // seeded as management_mode='router' by default

	body := map[string]any{
		"service_id": serviceID, "service": "pppoe", "ros_server_name": "test-pppoe", "interface": "ether1",
	}
	rec := httptest.NewRecorder()
	m.servicePlanHandler(rec, autoSetupRequest(t, "POST", "/services/plan", n.ID, body))
	if rec.Code != http.StatusConflict {
		t.Fatalf("plan against a router-managed service_id = %d, want 409 not_adopted: %s", rec.Code, rec.Body)
	}
	if got := router.writeCount(); got != 0 {
		t.Fatalf("expected zero router I/O when edit is refused pre-adopt, got %d writes", got)
	}
}

func TestServiceAdopt_FlipsModeWithZeroRouterWrites(t *testing.T) {
	m := autoSetupTestModule(t)
	router := newFakeRouter()
	m.dialROS = dialFakeRouter(router)
	n := mustInsertNAS(t, m, "admin", "pass", "7")
	serviceID := firstServiceID(t, m, n.ID)

	rec := httptest.NewRecorder()
	m.adoptServiceHandler(rec, serviceRequest(t, "POST", "/adopt", n.ID, serviceID, map[string]any{"confirm": true}))
	if rec.Code != http.StatusOK {
		t.Fatalf("adopt = %d: %s", rec.Code, rec.Body)
	}
	if got := serviceManagementMode(t, m, serviceID); got != "system" {
		t.Fatalf("expected management_mode=system after adopt, got %q", got)
	}
	if got := router.writeCount(); got != 0 {
		t.Fatalf("adopt must never write to the router, got %d writes: %+v", got, router.writes)
	}
}

func TestServiceAdopt_RequiresConfirm(t *testing.T) {
	m := autoSetupTestModule(t)
	n := mustInsertNAS(t, m, "admin", "pass", "7")
	serviceID := firstServiceID(t, m, n.ID)

	rec := httptest.NewRecorder()
	m.adoptServiceHandler(rec, serviceRequest(t, "POST", "/adopt", n.ID, serviceID, map[string]any{}))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("adopt without confirm = %d, want 422: %s", rec.Code, rec.Body)
	}
	if got := serviceManagementMode(t, m, serviceID); got != "router" {
		t.Fatalf("expected management_mode to stay router without confirm, got %q", got)
	}
}

func TestServiceAdopt_AlreadySystem_Returns409(t *testing.T) {
	m := autoSetupTestModule(t)
	n := mustInsertNAS(t, m, "admin", "pass", "7")
	serviceID := firstServiceID(t, m, n.ID)
	if err := setServiceManagementMode(context.Background(), m.db, n.ID, serviceID, "system"); err != nil {
		t.Fatalf("seed system mode: %v", err)
	}

	rec := httptest.NewRecorder()
	m.adoptServiceHandler(rec, serviceRequest(t, "POST", "/adopt", n.ID, serviceID, map[string]any{"confirm": true}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("adopt an already-system service = %d, want 409: %s", rec.Code, rec.Body)
	}
}
