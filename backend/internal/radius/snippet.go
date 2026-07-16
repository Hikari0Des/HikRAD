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
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/radius/vendor"
	"github.com/jackc/pgx/v5"
)

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

	// Type is the coarse kind the v1 single-service snippet renders. Chunk 3 of
	// this phase replaces it with the full per-instance Services loop (C8); for
	// now the first enabled instance preserves v1's exact output for the
	// one-service NAS that every upgraded install starts with.
	kind := "pppoe"
	if len(services) > 0 {
		kind = services[0].Service
	}

	return vendor.SnippetInput{
		ROSVersion:   ros,
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
