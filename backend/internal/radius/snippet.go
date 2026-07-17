package radius

// Config wizard backend (FR-14): GET /api/v1/nas/{id}/config-snippet?ros=6|7
// renders the copy-paste RouterOS bootstrap via the vendor adapter. The
// RADIUS server address the router should target comes from HIKRAD_RADIUS_HOST
// (the reachable FreeRADIUS host), defaulting to a placeholder Ali edits.

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/radius/vendor"
	"github.com/jackc/pgx/v5"
)

// snippetValuesFromQuery parses the FR-66.1 form overrides from query
// parameters (contract C3: "the copy-paste snippet endpoint accepts the same
// overrides so both paths describe one desired state"). A GET request has no
// JSON body by convention, so overrides travel as query params here instead
// of the JSON object the auto-setup POST endpoints use — same fields, same
// nil-means-default semantics.
func snippetValuesFromQuery(q map[string][]string) autoSetupValuesInput {
	var v autoSetupValuesInput
	get := func(key string) (string, bool) {
		vals, ok := q[key]
		if !ok || len(vals) == 0 {
			return "", false
		}
		return vals[0], true
	}
	if s, ok := get("radius_server"); ok {
		v.RadiusServer = &s
	}
	if s, ok := get("src_address"); ok {
		v.SrcAddress = &s
	}
	if s, ok := get("coa_port"); ok {
		if n, err := strconv.Atoi(s); err == nil {
			v.CoAPort = &n
		}
	}
	if s, ok := get("interim_secs"); ok {
		if n, err := strconv.Atoi(s); err == nil {
			v.InterimSecs = &n
		}
	}
	if s, ok := get("walled_garden"); ok {
		hosts := strings.Split(s, ",")
		v.WalledGarden = &hosts
	}
	return v
}

func (m *module) configSnippetHandler(w http.ResponseWriter, r *http.Request) {
	n, err := getNAS(r.Context(), m.db, chi.URLParam(r, "id"))
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "nas not found")
		return
	}
	if err != nil {
		m.internal(w, "get nas", err)
		return
	}

	in, ros, err := m.snippetInputFor(r.Context(), n, r.URL.Query().Get("ros"))
	if err != nil {
		m.internal(w, "build snippet input", err)
		return
	}
	in = snippetValuesFromQuery(r.URL.Query()).apply(in)
	snippet, err := vendor.For(n.Vendor).Snippet(in)
	if err != nil {
		m.internal(w, "render snippet", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"nas_id":      n.ID,
		"ros_version": ros,
		"type":        in.Type,
		"snippet":     snippet,
	})
}

// snippetInputFor builds the FR-14.2 desired-state input shared by the
// copy-paste snippet (this file) and the FR-56.2 auto-setup planner
// (autosetup_api.go) — both must describe exactly the same target config, or
// a router set up by one path would look "wrong" to the other. rosOverride,
// when non-empty, wins over the NAS record's stored ros_version (default 7).
func (m *module) snippetInputFor(ctx context.Context, n nasRow, rosOverride string) (vendor.SnippetInput, string, error) {
	services, err := enabledServices(ctx, m.db, n.ID)
	if err != nil {
		return vendor.SnippetInput{}, "", err
	}
	ros := rosOverride
	if ros == "" && n.ROSVersion != nil {
		ros = *n.ROSVersion
	}
	if ros != "6" && ros != "7" {
		ros = "7"
	}

	// The router must reach FreeRADIUS's auth/acct ports; the address is a
	// deployment fact, not a per-NAS one.
	server := os.Getenv("HIKRAD_RADIUS_HOST")
	if server == "" {
		server = "RADIUS_SERVER_IP" // placeholder for the operator to replace
	}

	// The NAS secret is shown here (FR-13.3: revealed to the operator setting up
	// the router); callers are nas.view/nas.edit-gated. Decrypt just for the render.
	secret := "<secret>"
	if plain, derr := decryptToString(n.SecretEnc); derr == nil {
		secret = plain
	}

	// Every enabled instance goes into one snippet (C8/FR-62): a router running
	// PPPoE plus two hotspot zones is configured by a single paste.
	svcs := make([]vendor.ServiceSnippet, 0, len(services))
	for _, s := range services {
		svcs = append(svcs, vendor.ServiceSnippet{
			Service:       s.Service,
			Label:         s.Label,
			ROSServerName: s.ROSServerName,
			PoolName:      s.IPPoolName,
			Interface:     s.InterfaceNote,
		})
	}
	// Type stays filled with the first instance's kind for the response's
	// legacy `type` field and any caller still reading it.
	kind := "pppoe"
	if len(services) > 0 {
		kind = services[0].Service
	}

	return vendor.SnippetInput{
		ROSVersion:   ros,
		Services:     svcs,
		Type:         kind,
		NASName:      n.Name,
		RadiusServer: server,
		Secret:       secret,
		CoAPort:      n.CoAPort,
		InterimSecs:  300,
		WalledGarden: defaultWalledGarden(),
	}, ros, nil
}

// defaultWalledGarden returns the hosts a Hotspot NAS must allow so the portal,
// payment and expired-redirect pages load before login (FR-14.2 / FR-18.2).
// Sourced from HIKRAD_PORTAL_HOSTS (comma-separated) when set.
func defaultWalledGarden() []string {
	raw := os.Getenv("HIKRAD_PORTAL_HOSTS")
	if raw == "" {
		return nil
	}
	var out []string
	for _, h := range strings.Split(raw, ",") {
		if h = strings.TrimSpace(h); h != "" {
			out = append(out, h)
		}
	}
	return out
}
