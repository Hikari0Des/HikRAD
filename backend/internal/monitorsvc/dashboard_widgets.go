package monitorsvc

// Dashboard widget catalog (v2-10, FR-89.1/89.2, contract C1). The single
// source of truth both GET /api/v1/dashboard's ?widgets= filter (C3) and the
// panel's picker read from (mirrored panel-side, never invented there).

import "github.com/hikrad/hikrad/internal/auth"

type dashboardWidgetSize string

const (
	widgetSize1x dashboardWidgetSize = "1x"
	widgetSize2x dashboardWidgetSize = "2x"
)

type dashboardWidgetDef struct {
	ID string
	// Permission is checked via Manager.Can; "" means every authenticated
	// manager, no permission needed (contract C1's my-balance row).
	Permission string
	Size       dashboardWidgetSize
}

// dashboardWidgetCatalog is C1's frozen table, in the phase brief's own row
// order — also the default-layout order (FR-90.1).
var dashboardWidgetCatalog = []dashboardWidgetDef{
	{ID: "online-now", Permission: "live.view", Size: widgetSize1x},
	{ID: "revenue-today", Permission: "reports.view", Size: widgetSize1x},
	{ID: "radius-rps", Permission: PermView, Size: widgetSize1x},
	{ID: "subs-active", Permission: "subscribers.view", Size: widgetSize1x},
	{ID: "subs-expired", Permission: "subscribers.view", Size: widgetSize1x},
	{ID: "subs-expiring", Permission: "subscribers.view", Size: widgetSize1x},
	{ID: "pipeline-health", Permission: PermView, Size: widgetSize2x},
	{ID: "nas-health", Permission: "nas.view", Size: widgetSize2x},
	{ID: "my-balance", Permission: "", Size: widgetSize1x},
	{ID: "pending-payment-tickets", Permission: "payment_tickets.verify", Size: widgetSize1x},
	{ID: "alerts-feed", Permission: PermView, Size: widgetSize2x},
}

// widgetPermitted reports whether m (nil-safe: an unauthenticated caller
// holds only the no-permission-required widgets) may see w.
func widgetPermitted(m *auth.Manager, w dashboardWidgetDef) bool {
	if w.Permission == "" {
		return true
	}
	if m == nil {
		return false
	}
	return m.Can(w.Permission)
}

// filterDashboardWidgets returns the subset of requested ids that are both
// known catalog entries and permitted for m, in CATALOG order (not the
// caller's order) — the same canonical order the default layout uses, so a
// partial request and the default layout are always internally consistent.
// An unknown or forbidden id is silently dropped, never an error (FR-89.3).
func filterDashboardWidgets(m *auth.Manager, requested []string) []string {
	want := make(map[string]bool, len(requested))
	for _, id := range requested {
		want[id] = true
	}
	out := make([]string, 0, len(requested))
	for _, w := range dashboardWidgetCatalog {
		if want[w.ID] && widgetPermitted(m, w) {
			out = append(out, w.ID)
		}
	}
	return out
}

// needsSubsQuery reports whether any of the three subs-* ids is present —
// they share one query (subscriberTiles), so requesting any one of them
// computes and returns the full subs object (C3's frozen exception).
func needsSubsQuery(ids []string) bool {
	for _, id := range ids {
		if id == "subs-active" || id == "subs-expired" || id == "subs-expiring" {
			return true
		}
	}
	return false
}

func containsWidget(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
