package subscribers

// Subscriber CRUD (FR-1) with manager scoping (FR-27.2), phone normalization
// (FR-1.3), static-IP validation against B's static pools (FR-16.2), per-user
// overrides (FR-7) and the FR-58/FR-55 toggles. Every mutation writes the audit
// log (C2) and invalidates B's policy cache (C4).

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform/crypto"
	"github.com/hikrad/hikrad/internal/radius"
)

// writeInput is the create/update body. Pointers distinguish "field omitted"
// (leave/clear per handler) from a zero value; username is create-only.
type writeInput struct {
	Username             string     `json:"username"`
	Password             *string    `json:"password"` // create: required; update: reset if present
	Name                 *string    `json:"name"`
	Phone                *string    `json:"phone"`
	Address              *string    `json:"address"`
	Notes                *string    `json:"notes"`
	Status               *string    `json:"status"`
	ProfileID            *string    `json:"profile_id"`
	OwnerManagerID       *string    `json:"owner_manager_id"`
	ExpiresAt            *time.Time `json:"expires_at"`
	MacLockMode          *string    `json:"mac_lock_mode"`
	StaticIP             *string    `json:"static_ip"`
	SessionLimitOverride *int       `json:"session_limit_override"`
	RateOverride         *string    `json:"rate_override"`
	PriceOverride        *int64     `json:"price_override"`
	DisabledReason       *string    `json:"disabled_reason"`
	AllowHotspot         *bool      `json:"allow_hotspot"`
	WhatsappOptIn        *bool      `json:"whatsapp_opt_in"`
	// NoPassword (item 13): hotspot subscribers may deliberately have no
	// password — the NAS then sends password="" and auth compares empty to
	// empty. true seals an empty credential (and clears any existing one);
	// portal password login is refused for such accounts.
	NoPassword *bool `json:"no_password"`
}

func (m *Module) listHandler(w http.ResponseWriter, r *http.Request) {
	page, err := httpapi.ParsePage(r)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", err.Error())
		return
	}
	var after *string
	if len(page.Cursor) == 1 {
		after = &page.Cursor[0]
	} else if page.Cursor != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", "malformed cursor")
		return
	}
	scope := auth.ScopeFilter(r.Context())
	// $1 cursor, $2 limit, optional $3 owner scope.
	q := `SELECT ` + columns + ` FROM subscribers WHERE ($1::uuid IS NULL OR id > $1::uuid)`
	args := []any{after}
	clause, arg := scopeClause(scope, 3)
	q += clause
	q += ` ORDER BY id LIMIT $2`
	args = append(args, page.Limit+1)
	if arg != nil {
		args = append(args, arg)
	}
	rows, err := m.db.Query(r.Context(), q, args...)
	if err != nil {
		m.internalError(w, "list", err)
		return
	}
	defer rows.Close()
	items := make([]Subscriber, 0, page.Limit)
	for rows.Next() {
		s, err := scanSubscriber(rows)
		if err != nil {
			m.internalError(w, "list scan", err)
			return
		}
		items = append(items, s)
	}
	if rows.Err() != nil {
		m.internalError(w, "list rows", rows.Err())
		return
	}
	next := ""
	if len(items) > page.Limit {
		items = items[:page.Limit]
		next = httpapi.EncodeCursor(items[len(items)-1].ID)
	}
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(items, next))
}

func (m *Module) getHandler(w http.ResponseWriter, r *http.Request) {
	s, err := getByID(r.Context(), m.db, chi.URLParam(r, "id"), auth.ScopeFilter(r.Context()))
	if isNotFound(err) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "subscriber not found")
		return
	}
	if err != nil {
		m.internalError(w, "get", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, s)
}

func (m *Module) createHandler(w http.ResponseWriter, r *http.Request) {
	var in writeInput
	if !httpapi.Bind(w, r, &in) {
		return
	}
	fe := []httpapi.FieldError{}
	if strings.TrimSpace(in.Username) == "" {
		fe = append(fe, httpapi.FieldError{Field: "username", Message: "this field is required"})
	}
	noPassword := in.NoPassword != nil && *in.NoPassword
	if !noPassword && (in.Password == nil || *in.Password == "") {
		fe = append(fe, httpapi.FieldError{Field: "password", Message: "this field is required"})
	}
	norm, feMore := m.normalizeWrite(r.Context(), &in, "")
	fe = append(fe, feMore...)
	if len(fe) > 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}

	plainPassword := ""
	if !noPassword {
		plainPassword = *in.Password
	}
	enc, err := crypto.Encrypt([]byte(plainPassword))
	if err != nil {
		m.internalError(w, "seal password", err)
		return
	}

	// A scoped caller (agent) always owns what they create; an unscoped caller
	// may target a specific owner or leave it null (unscoped visibility).
	owner := norm.owner
	if scope := auth.ScopeFilter(r.Context()); scope != nil {
		owner = &scope.ManagerID
	}

	status := "active"
	if in.Status != nil && *in.Status != "" {
		status = *in.Status
	}
	// mac_lock_mode is NOT NULL; default to 'off' when the field is omitted.
	macMode := "off"
	if norm.macLockMode != nil {
		macMode = *norm.macLockMode
	}

	s, err := scanSubscriber(m.db.QueryRow(r.Context(),
		`INSERT INTO subscribers
		   (username, password_enc, name, phone, address, notes, status, profile_id,
		    owner_manager_id, expires_at, mac_lock_mode, static_ip, session_limit_override,
		    rate_override, price_override, disabled_reason, allow_hotspot, whatsapp_opt_in,
		    has_password, quota_cycle_anchor)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8::uuid,$9::uuid,$10,$11,$12::inet,$13,$14,$15,$16,$17,$18,$19, now())
		 RETURNING `+columns,
		in.Username, enc, in.Name, norm.phonePtr, in.Address, in.Notes, status, in.ProfileID,
		owner, in.ExpiresAt, macMode, norm.staticIP, in.SessionLimitOverride,
		in.RateOverride, in.PriceOverride, in.DisabledReason, boolOr(in.AllowHotspot, false),
		boolOr(in.WhatsappOptIn, false), !noPassword))
	if err != nil {
		if isUniqueViolation(err) {
			httpapi.Error(w, http.StatusConflict, "conflict", "username already exists")
			return
		}
		if isForeignKeyViolation(err) {
			httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
				httpapi.FieldError{Field: "profile_id", Message: "unknown profile"})
			return
		}
		m.internalError(w, "create", err)
		return
	}
	_ = auth.Audit(r.Context(), "subscriber.create", "subscriber", s.ID, nil, s)
	_ = radius.InvalidatePolicy(s.ID)
	httpapi.JSON(w, http.StatusCreated, s)
}

func (m *Module) updateHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	scope := auth.ScopeFilter(r.Context())
	before, err := getByID(r.Context(), m.db, id, scope)
	if isNotFound(err) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "subscriber not found")
		return
	}
	if err != nil {
		m.internalError(w, "get for update", err)
		return
	}
	var in writeInput
	if !httpapi.Bind(w, r, &in) {
		return
	}
	// Username is immutable (it is the RADIUS identity) — reject a change attempt.
	if in.Username != "" && !strings.EqualFold(in.Username, before.Username) {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "username", Message: "username is immutable"})
		return
	}
	// A scoped caller cannot reassign ownership away from themselves.
	if scope != nil {
		in.OwnerManagerID = nil
	}
	norm, fe := m.normalizeWrite(r.Context(), &in, id)
	if len(fe) > 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}

	// COALESCE-style partial update: a nil pointer leaves the column unchanged;
	// password reseals only when provided. static_ip/mac_lock are pre-normalized.
	// no_password (item 13) wins over a password value: it seals an empty
	// credential and flips has_password off; a provided password flips it on.
	var encArg any
	var hasPasswordArg *bool
	switch {
	case in.NoPassword != nil && *in.NoPassword:
		enc, err := crypto.Encrypt([]byte(""))
		if err != nil {
			m.internalError(w, "seal password", err)
			return
		}
		encArg = enc
		hasPasswordArg = boolPtr(false)
	case in.Password != nil && *in.Password != "":
		enc, err := crypto.Encrypt([]byte(*in.Password))
		if err != nil {
			m.internalError(w, "seal password", err)
			return
		}
		encArg = enc
		hasPasswordArg = boolPtr(true)
	}

	after, err := scanSubscriber(m.db.QueryRow(r.Context(),
		`UPDATE subscribers SET
		    password_enc          = COALESCE($2, password_enc),
		    name                  = CASE WHEN $3::bool THEN $4 ELSE name END,
		    phone                 = CASE WHEN $5::bool THEN $6 ELSE phone END,
		    address               = CASE WHEN $7::bool THEN $8 ELSE address END,
		    notes                 = CASE WHEN $9::bool THEN $10 ELSE notes END,
		    status                = COALESCE($11, status),
		    profile_id            = CASE WHEN $12::bool THEN $13::uuid ELSE profile_id END,
		    owner_manager_id      = CASE WHEN $14::bool THEN $15::uuid ELSE owner_manager_id END,
		    expires_at            = CASE WHEN $16::bool THEN $17 ELSE expires_at END,
		    mac_lock_mode         = COALESCE($18, mac_lock_mode),
		    static_ip             = CASE WHEN $19::bool THEN $20::inet ELSE static_ip END,
		    session_limit_override= CASE WHEN $21::bool THEN $22 ELSE session_limit_override END,
		    rate_override         = CASE WHEN $23::bool THEN $24 ELSE rate_override END,
		    price_override        = CASE WHEN $25::bool THEN $26 ELSE price_override END,
		    disabled_reason       = CASE WHEN $27::bool THEN $28 ELSE disabled_reason END,
		    allow_hotspot         = COALESCE($29, allow_hotspot),
		    whatsapp_opt_in       = COALESCE($30, whatsapp_opt_in),
		    has_password          = COALESCE($31, has_password)
		  WHERE id = $1::uuid
		 RETURNING `+columns,
		id, encArg,
		in.Name != nil, in.Name,
		in.Phone != nil, norm.phonePtr,
		in.Address != nil, in.Address,
		in.Notes != nil, in.Notes,
		nilIfEmpty(in.Status), in.ProfileID != nil, in.ProfileID,
		in.OwnerManagerID != nil, in.OwnerManagerID,
		in.ExpiresAt != nil, in.ExpiresAt,
		nilIfEmpty(in.MacLockMode), in.StaticIP != nil, norm.staticIP,
		in.SessionLimitOverride != nil, in.SessionLimitOverride,
		in.RateOverride != nil, in.RateOverride,
		in.PriceOverride != nil, in.PriceOverride,
		in.DisabledReason != nil, in.DisabledReason,
		in.AllowHotspot, in.WhatsappOptIn, hasPasswordArg))
	if err != nil {
		if isForeignKeyViolation(err) {
			httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
				httpapi.FieldError{Field: "profile_id", Message: "unknown profile"})
			return
		}
		m.internalError(w, "update", err)
		return
	}
	_ = auth.Audit(r.Context(), "subscriber.update", "subscriber", id, before, after)
	_ = radius.InvalidatePolicy(id)

	// Disabling an online user offers an immediate CoA disconnect (FR-1.2); the
	// panel acts on this flag. We surface it rather than disconnecting here so the
	// operator confirms (and Phase 3 owns the enforcement worker).
	resp := map[string]any{"subscriber": after}
	if after.Status == "disabled" && before.Status != "disabled" {
		resp["offer_disconnect"] = true
	}
	httpapi.JSON(w, http.StatusOK, resp)
}

func (m *Module) deleteHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	scope := auth.ScopeFilter(r.Context())
	before, err := getByID(r.Context(), m.db, id, scope)
	if isNotFound(err) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "subscriber not found")
		return
	}
	if err != nil {
		m.internalError(w, "get for delete", err)
		return
	}
	if err := deleteByID(r.Context(), m.db, id, scope); err != nil {
		if isNotFound(err) {
			httpapi.Error(w, http.StatusNotFound, "not_found", "subscriber not found")
			return
		}
		m.internalError(w, "delete", err)
		return
	}
	_ = auth.Audit(r.Context(), "subscriber.delete", "subscriber", id, before, nil)
	_ = radius.InvalidatePolicy(id)
	w.WriteHeader(http.StatusNoContent)
}

// normalized carries the validated/transformed write fields.
type normalized struct {
	phonePtr    *string
	staticIP    *string
	macLockMode *string
	owner       *string
}

// normalizeWrite validates and canonicalizes the cross-cutting fields shared by
// create and update: phone (FR-1.3), static IP (FR-16.2), mac_lock_mode enum,
// status enum, and the whatsapp-opt-in→phone dependency (FR-1.5). excludeID is
// the subscriber being updated ("" on create) for static-IP uniqueness.
func (m *Module) normalizeWrite(ctx context.Context, in *writeInput, excludeID string) (normalized, []httpapi.FieldError) {
	var out normalized
	var fe []httpapi.FieldError
	add := func(f, msg string) { fe = append(fe, httpapi.FieldError{Field: f, Message: msg}) }

	if in.Phone != nil {
		p, ok := normalizePhone(*in.Phone)
		if !ok {
			add("phone", "not a valid Iraqi mobile number")
		} else {
			out.phonePtr = strPtrOrNil(p)
		}
	}
	// whatsapp_opt_in requires a valid phone (FR-1.5). Consider both the incoming
	// phone and (on update) the existing one — but a create with opt-in and no
	// phone is rejected.
	if boolOr(in.WhatsappOptIn, false) && in.Phone != nil && out.phonePtr == nil {
		add("whatsapp_opt_in", "a valid phone is required to enable WhatsApp messaging")
	}
	if in.WhatsappOptIn != nil && *in.WhatsappOptIn && in.Phone == nil && excludeID == "" {
		add("whatsapp_opt_in", "a valid phone is required to enable WhatsApp messaging")
	}

	if in.MacLockMode != nil && *in.MacLockMode != "" {
		switch *in.MacLockMode {
		case "off", "learn", "fixed":
			out.macLockMode = in.MacLockMode
		default:
			add("mac_lock_mode", "must be one of: off learn fixed")
		}
	}

	if in.Status != nil && *in.Status != "" {
		switch *in.Status {
		case "active", "disabled", "expired":
		default:
			add("status", "must be one of: active disabled expired")
		}
	}

	if in.StaticIP != nil && *in.StaticIP != "" {
		ip := strings.TrimSpace(*in.StaticIP)
		inPool, err := radius.StaticIPInStaticPool(ctx, ip)
		if err != nil {
			m.log.Error("subscribers: static-ip pool check", "error", err)
			add("static_ip", "could not validate against IP pools")
		} else if !inPool {
			add("static_ip", "must fall within a static-purpose IP pool")
		} else if dup, err := existsStaticIP(ctx, m.db, ip, excludeID); err != nil {
			m.log.Error("subscribers: static-ip uniqueness check", "error", err)
			add("static_ip", "could not validate uniqueness")
		} else if dup {
			add("static_ip", "already assigned to another subscriber")
		} else {
			out.staticIP = &ip
		}
	} else if in.StaticIP != nil {
		// Explicit empty string clears the static IP.
		out.staticIP = nil
	}

	if in.OwnerManagerID != nil && *in.OwnerManagerID != "" {
		out.owner = in.OwnerManagerID
	}
	return out, fe
}

func boolOr(p *bool, d bool) bool {
	if p == nil {
		return d
	}
	return *p
}

func boolPtr(b bool) *bool { return &b }

func nilIfEmpty(p *string) *string {
	if p == nil || *p == "" {
		return nil
	}
	return p
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
