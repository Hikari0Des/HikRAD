package radius

// Config wizard backend (FR-14): GET /api/v1/nas/{id}/config-snippet?ros=6|7
// renders the copy-paste RouterOS bootstrap via the vendor adapter. The
// RADIUS server address the router should target comes from HIKRAD_RADIUS_HOST
// (the reachable FreeRADIUS host), defaulting to a placeholder Ali edits.

import (
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

	// ?ros overrides the stored version; otherwise use the NAS record, default 7.
	ros := r.URL.Query().Get("ros")
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
	// the router); this endpoint is nas.view-gated. Decrypt just for the render.
	secret := "<secret>"
	if plain, derr := decryptToString(n.SecretEnc); derr == nil {
		secret = plain
	}

	snippet, err := vendor.For(n.Vendor).Snippet(vendor.SnippetInput{
		ROSVersion:   ros,
		Type:         n.Type,
		NASName:      n.Name,
		RadiusServer: server,
		Secret:       secret,
		CoAPort:      n.CoAPort,
		InterimSecs:  300,
		WalledGarden: defaultWalledGarden(),
	})
	if err != nil {
		m.internal(w, "render snippet", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"nas_id":      n.ID,
		"ros_version": ros,
		"type":        n.Type,
		"snippet":     snippet,
	})
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
