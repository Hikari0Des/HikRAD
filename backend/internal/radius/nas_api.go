package radius

// NAS registry REST handlers (C7-B / FR-13). Mutations are audited (C2) and
// regenerate the FreeRADIUS clients file + invalidate the known-NAS cache so a
// change takes effect with no restart (AC-13a).

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/radius/vendor"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// (imports intentionally minimal; see handlers below)

// nasRequest is the create/update body. Secret/SNMP carry audit:"secret" so
// they are redacted from the audit log (C2). On update an empty secret leaves
// the sealed value untouched.
type nasRequest struct {
	Name          string  `json:"name" validate:"required,min=1,max=128"`
	IP            string  `json:"ip" validate:"required,ip"`
	Secret        string  `json:"secret" audit:"secret"`
	Type          string  `json:"type" validate:"omitempty,oneof=pppoe hotspot"`
	Vendor        string  `json:"vendor" validate:"omitempty,max=32"`
	CoAPort       int     `json:"coa_port" validate:"omitempty,min=1,max=65535"`
	SNMPCommunity string  `json:"snmp_community" audit:"secret"`
	ROSVersion    *string `json:"ros_version" validate:"omitempty"`
	Location      string  `json:"location" validate:"omitempty,max=256"`
	Enabled       *bool   `json:"enabled"`
	// APIPort/APIUser/APIPassword (FR-56.2): RouterOS API auto-setup
	// credentials, encrypted at rest like Secret/SNMPCommunity. Optional —
	// a NAS with none set simply can't preview/apply auto-setup and stays on
	// the FR-14 copy-paste path.
	APIPort     int    `json:"api_port" validate:"omitempty,min=1,max=65535"`
	APIUser     string `json:"api_user" validate:"omitempty,max=128"`
	APIPassword string `json:"api_password" audit:"secret"`
}

func (req nasRequest) toInput() nasInput {
	in := nasInput{
		Name: req.Name, IP: req.IP, Secret: req.Secret, Type: req.Type,
		Vendor: req.Vendor, CoAPort: req.CoAPort, SNMP: req.SNMPCommunity,
		ROSVersion: req.ROSVersion, Location: req.Location, Enabled: true,
		APIPort: req.APIPort, APIUser: req.APIUser, APIPassword: req.APIPassword,
	}
	if in.Type == "" {
		in.Type = "pppoe"
	}
	if in.Vendor == "" {
		in.Vendor = "mikrotik"
	}
	if in.CoAPort == 0 {
		in.CoAPort = 3799
	}
	if req.Enabled != nil {
		in.Enabled = *req.Enabled
	}
	return in
}

func (m *module) listNASHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := listNAS(r.Context(), m.db)
	if err != nil {
		m.internal(w, "list nas", err)
		return
	}
	views := make([]nasView, 0, len(rows))
	for _, n := range rows {
		views = append(views, n.view())
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": views})
}

func (m *module) getNASHandler(w http.ResponseWriter, r *http.Request) {
	n, err := getNAS(r.Context(), m.db, chi.URLParam(r, "id"))
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "nas not found")
		return
	}
	if err != nil {
		m.internal(w, "get nas", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, n.view())
}

func (m *module) createNASHandler(w http.ResponseWriter, r *http.Request) {
	var req nasRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	if req.Secret == "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "secret is required",
			httpapi.FieldError{Field: "secret", Message: "this field is required"})
		return
	}
	n, err := insertNAS(r.Context(), m.db, req.toInput())
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			httpapi.Error(w, http.StatusConflict, "conflict", "a NAS with that IP already exists")
			return
		}
		m.internal(w, "insert nas", err)
		return
	}
	m.afterNASChange(r.Context())
	_ = auth.Audit(r.Context(), "nas.create", "nas", n.ID, nil, n.view())
	httpapi.JSON(w, http.StatusCreated, n.view())
}

func (m *module) updateNASHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	before, err := getNAS(r.Context(), m.db, id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "nas not found")
		return
	}
	if err != nil {
		m.internal(w, "lookup nas", err)
		return
	}
	var req nasRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	n, err := updateNAS(r.Context(), m.db, id, req.toInput(), req.Secret != "", req.SNMPCommunity != "", req.APIPassword != "")
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			httpapi.Error(w, http.StatusConflict, "conflict", "a NAS with that IP already exists")
			return
		}
		m.internal(w, "update nas", err)
		return
	}
	m.afterNASChange(r.Context())
	_ = auth.Audit(r.Context(), "nas.update", "nas", id, before.view(), n.view())
	httpapi.JSON(w, http.StatusOK, n.view())
}

// deleteNASHandler refuses to delete a NAS with live sessions unless ?confirm=
// true (FR-13.4); C then marks the orphaned sessions stale (FR-38).
func (m *module) deleteNASHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	before, err := getNAS(r.Context(), m.db, id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "nas not found")
		return
	}
	if err != nil {
		m.internal(w, "lookup nas", err)
		return
	}
	if r.URL.Query().Get("confirm") != "true" {
		if live := currentNASLiveCount()(id); live > 0 {
			httpapi.Error(w, http.StatusConflict, "confirmation_required",
				"this NAS has live sessions; retry with ?confirm=true to delete and mark them stale")
			return
		}
	}
	if err := deleteNAS(r.Context(), m.db, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpapi.Error(w, http.StatusNotFound, "not_found", "nas not found")
			return
		}
		m.internal(w, "delete nas", err)
		return
	}
	m.afterNASChange(r.Context())
	_ = auth.Audit(r.Context(), "nas.delete", "nas", id, before.view(), nil)
	w.WriteHeader(http.StatusNoContent)
}

// probeNASHandler (item 8 / FR-13): connects to the router over the RouterOS
// API with the NAS's saved credentials, reads version/board/identity
// (read-only print sentences), stores the fresh ros_version on the NAS row,
// and returns what it saw so the panel can fill the form. 422 without saved
// API credentials; 502 when the router doesn't answer.
func (m *module) probeNASHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	n, err := getNAS(ctx, m.db, chi.URLParam(r, "id"))
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "nas not found")
		return
	}
	if err != nil {
		m.internal(w, "get nas", err)
		return
	}
	if n.APIUser == "" || len(n.APIPasswordEnc) == 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "no_api_credentials",
			"no RouterOS API credentials saved for this NAS")
		return
	}
	apiPassword, err := decryptToString(n.APIPasswordEnc)
	if err != nil {
		m.internal(w, "decrypt nas api password", err)
		return
	}
	conn, err := m.dialROS(ctx, n.IP, apiPortOrDefault(n.APIPort), n.APIUser, apiPassword)
	if err != nil {
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable",
			"could not connect to the router: "+err.Error())
		return
	}
	defer conn.Close()
	info, err := vendor.ReadDeviceInfo(conn)
	if err != nil {
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable",
			"connected but could not read the router's version: "+err.Error())
		return
	}
	if _, err := m.db.Exec(ctx,
		`UPDATE nas SET ros_version = $2, updated_at = now() WHERE id = $1::uuid`,
		n.ID, info.Version); err != nil {
		m.internal(w, "store probed ros version", err)
		return
	}
	m.nas.invalidate()
	_ = auth.Audit(ctx, "nas.probe", "nas", n.ID, nil, map[string]any{
		"ros_version": info.Version, "board_name": info.BoardName, "identity": info.Identity,
	})
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"ros_version": info.Version, "board_name": info.BoardName, "identity": info.Identity,
	})
}

// nasStatusHandler reports the FR-14.4 "seen since created" check: the last
// Access-Request (this package) and last accounting packet (C) times for the
// NAS IP, from Redis.
func (m *module) nasStatusHandler(w http.ResponseWriter, r *http.Request) {
	n, err := getNAS(r.Context(), m.db, chi.URLParam(r, "id"))
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "nas not found")
		return
	}
	if err != nil {
		m.internal(w, "get nas", err)
		return
	}
	out := map[string]any{"id": n.ID, "ip": n.IP, "last_auth_at": nil, "last_acct_at": nil}
	if m.rdb != nil {
		ctx := r.Context()
		if v, e := m.rdb.Get(ctx, nasSeenAuthPrefix+canonicalIP(n.IP)).Result(); e == nil {
			out["last_auth_at"] = v
		}
		if v, e := m.rdb.Get(ctx, "nas:seen:acct:"+canonicalIP(n.IP)).Result(); e == nil {
			out["last_acct_at"] = v
		}
	}
	out["seen"] = out["last_auth_at"] != nil || out["last_acct_at"] != nil
	httpapi.JSON(w, http.StatusOK, out)
}

// afterNASChange refreshes the known-NAS cache and the FreeRADIUS clients file
// after any mutation.
func (m *module) afterNASChange(ctx context.Context) {
	m.nas.invalidate()
	// Detach so a slow disk write can't hold the API response.
	go m.regenerateClients(context.WithoutCancel(ctx))
}

func (m *module) internal(w http.ResponseWriter, what string, err error) {
	m.log.Error("radius: "+what+" failed", "error", err)
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
}
