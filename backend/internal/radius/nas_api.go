package radius

// NAS registry REST handlers (C7-B / FR-13). Mutations are audited (C2) and
// regenerate the FreeRADIUS clients file + invalidate the known-NAS cache so a
// change takes effect with no restart (AC-13a).

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

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
	Name   string `json:"name" validate:"required,min=1,max=128"`
	IP     string `json:"ip" validate:"required,ip"`
	Secret string `json:"secret" audit:"secret"`
	// Services is the NAS's service instances (FR-62 / C9) — it replaced v1's
	// single `type`. The array is the whole truth for the NAS: an omitted row
	// is deleted. At least one is required.
	Services      []serviceInput `json:"services"`
	Vendor        string         `json:"vendor" validate:"omitempty,max=32"`
	CoAPort       int            `json:"coa_port" validate:"omitempty,min=1,max=65535"`
	SNMPCommunity string         `json:"snmp_community" audit:"secret"`
	ROSVersion    *string        `json:"ros_version" validate:"omitempty"`
	Location      string         `json:"location" validate:"omitempty,max=256"`
	Enabled       *bool          `json:"enabled"`
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
		Name: req.Name, IP: req.IP, Secret: req.Secret,
		Vendor: req.Vendor, CoAPort: req.CoAPort, SNMP: req.SNMPCommunity,
		ROSVersion: req.ROSVersion, Location: req.Location, Enabled: true,
		APIPort: req.APIPort, APIUser: req.APIUser, APIPassword: req.APIPassword,
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

// validateServices enforces the C3/C9 write rules for the embedded services
// array before any transaction opens.
func validateServices(in []serviceInput) []httpapi.FieldError {
	var fe []httpapi.FieldError
	if len(in) == 0 {
		return []httpapi.FieldError{{Field: "services", Message: "at least one service is required"}}
	}
	// A hotspot request resolves to an instance by its RouterOS server name
	// (C7); two enabled hotspots sharing one name on the same NAS would be
	// indistinguishable at auth, so the sessions would reject as ambiguous.
	// Catch it in the form instead of at 2am on the router.
	seen := map[string]int{}
	for i, s := range in {
		switch s.Service {
		case "pppoe", "hotspot":
		default:
			fe = append(fe, httpapi.FieldError{
				Field: fmt.Sprintf("services.%d.service", i), Message: "must be one of: pppoe hotspot"})
			continue
		}
		if !s.enabled() {
			continue
		}
		key := s.Service + "\x00" + s.ROSServerName
		if prev, dup := seen[key]; dup {
			fe = append(fe, httpapi.FieldError{
				Field: fmt.Sprintf("services.%d.ros_server_name", i),
				Message: fmt.Sprintf(
					"another enabled %s service (#%d) already uses this server name; they could not be told apart at login",
					s.Service, prev+1)})
			continue
		}
		seen[key] = i
	}
	return fe
}

// withServices attaches each NAS's service instances (and their live counts) to
// its view. One query for the whole list, not one per NAS.
func (m *module) withServices(ctx context.Context, rows []nasRow) ([]nasView, error) {
	ids := make([]string, 0, len(rows))
	for _, n := range rows {
		ids = append(ids, n.ID)
	}
	byNAS, err := listServicesFor(ctx, m.db, ids)
	if err != nil {
		return nil, err
	}
	count := currentServiceLiveCount()
	views := make([]nasView, 0, len(rows))
	for _, n := range rows {
		v := n.view()
		for _, s := range byNAS[n.ID] {
			sv := s.view()
			sv.LiveSessions = count(s.ID)
			v.Services = append(v.Services, sv)
		}
		views = append(views, v)
	}
	return views, nil
}

func (m *module) listNASHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := listNAS(r.Context(), m.db)
	if err != nil {
		m.internal(w, "list nas", err)
		return
	}
	views, err := m.withServices(r.Context(), rows)
	if err != nil {
		m.internal(w, "list nas services", err)
		return
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
	views, err := m.withServices(r.Context(), []nasRow{n})
	if err != nil {
		m.internal(w, "get nas services", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, views[0])
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
	if fe := validateServices(req.Services); len(fe) > 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}
	// The NAS row and its services commit together (C3: a NAS with no service
	// authenticates nobody, so a half-written one is a dead router).
	var n nasRow
	err := m.inTx(r.Context(), func(tx pgx.Tx) error {
		var err error
		if n, err = insertNAS(r.Context(), tx, req.toInput()); err != nil {
			return err
		}
		return replaceServices(r.Context(), tx, n.ID, req.Services)
	})
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
	views, err := m.withServices(r.Context(), []nasRow{n})
	if err != nil {
		m.internal(w, "read back nas services", err)
		return
	}
	_ = auth.Audit(r.Context(), "nas.create", "nas", n.ID, nil, views[0])
	httpapi.JSON(w, http.StatusCreated, views[0])
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
	beforeViews, err := m.withServices(r.Context(), []nasRow{before})
	if err != nil {
		m.internal(w, "lookup nas services", err)
		return
	}
	var req nasRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	if fe := validateServices(req.Services); len(fe) > 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}
	var n nasRow
	err = m.inTx(r.Context(), func(tx pgx.Tx) error {
		var err error
		n, err = updateNAS(r.Context(), tx, id, req.toInput(),
			req.Secret != "", req.SNMPCommunity != "", req.APIPassword != "")
		if err != nil {
			return err
		}
		return replaceServices(r.Context(), tx, id, req.Services)
	})
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
	views, err := m.withServices(r.Context(), []nasRow{n})
	if err != nil {
		m.internal(w, "read back nas services", err)
		return
	}
	_ = auth.Audit(r.Context(), "nas.update", "nas", id, beforeViews[0], views[0])
	httpapi.JSON(w, http.StatusOK, views[0])
}

// inTx runs fn in a transaction, rolling back on error.
func (m *module) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
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

// discoveredServiceView is one router-reported service instance (FR-62.6).
// `matched_service_id` links it to an existing nas_services row so the panel can
// show "already imported" instead of offering a duplicate.
type discoveredServiceView struct {
	Service          string `json:"service"`
	ROSServerName    string `json:"ros_server_name"`
	Label            string `json:"label"`
	InterfaceNote    string `json:"interface_note"`
	RouterPoolName   string `json:"router_pool_name"`
	Enabled          bool   `json:"enabled"`
	MatchedServiceID string `json:"matched_service_id"`
}

// discoverServicesHandler (FR-62.6) reads the router's real PPPoE/Hotspot
// service instances over the RouterOS API and returns them for the operator to
// import — instead of hand-typing names that must match the router exactly.
//
// READ-ONLY on both sides: it issues only print sentences (never add/set), and
// it writes nothing to HikRAD either. It proposes; the operator confirms in the
// NAS form and saves through the normal services[] contract, so a discovery
// mistake can never silently rewrite a working NAS.
//
// 422 without saved API credentials; 502 when the router doesn't answer —
// matching probeNASHandler, which this deliberately mirrors.
func (m *module) discoverServicesHandler(w http.ResponseWriter, r *http.Request) {
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

	found, err := vendor.For(n.Vendor).DiscoverServices(conn)
	if err != nil {
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable",
			"connected but could not read the router's services: "+err.Error())
		return
	}

	// Match against what the NAS already has so a re-run is idempotent in the
	// panel: an already-imported instance is shown as such rather than offered
	// again as a new row (which would collide on ros_server_name anyway).
	existing, err := listServices(ctx, m.db, n.ID)
	if err != nil {
		m.internal(w, "list nas services", err)
		return
	}
	byKey := make(map[string]string, len(existing))
	for _, s := range existing {
		byKey[s.Service+"\x00"+strings.ToLower(s.ROSServerName)] = s.ID
	}

	items := make([]discoveredServiceView, 0, len(found))
	for _, f := range found {
		items = append(items, discoveredServiceView{
			Service:          f.Service,
			ROSServerName:    f.ROSServerName,
			Label:            f.Label,
			InterfaceNote:    f.Interface,
			RouterPoolName:   f.PoolName,
			Enabled:          !f.Disabled,
			MatchedServiceID: byKey[f.Service+"\x00"+strings.ToLower(f.ROSServerName)],
		})
	}
	// Health findings (FR-62.7) ride along with discovery: the operator is
	// already looking at this router, and these are the conditions that make a
	// perfectly good HikRAD config fail anyway. Best-effort — a health probe that
	// errors must never fail the discovery the operator actually asked for.
	health, err := vendor.For(n.Vendor).CheckHealth(conn)
	if err != nil {
		m.log.Warn("radius: nas health check failed", "error", err, "nas", n.ID)
		health = nil
	}
	if health == nil {
		health = []vendor.HealthFinding{}
	}

	_ = auth.Audit(ctx, "nas.discover_services", "nas", n.ID, nil,
		map[string]any{"found": len(items), "health_findings": len(health)})
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items, "health": health})
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
