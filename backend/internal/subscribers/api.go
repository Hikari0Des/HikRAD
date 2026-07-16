package subscribers

// Subscriber CRUD (FR-1) with manager scoping (FR-27.2), phone normalization
// (FR-1.3), static-IP validation against B's static pools (FR-16.2), per-user
// overrides (FR-7) and the FR-58/FR-55 toggles. Every mutation writes the audit
// log (C2) and invalidates B's policy cache (C4).

import (
	"context"
	"fmt"
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
	// ServiceType (FR-61) is pppoe | hotspot | dual; it replaced v1's
	// allow_hotspot bool. Omitted on create defaults to pppoe.
	ServiceType   *string `json:"service_type"`
	WhatsappOptIn *bool   `json:"whatsapp_opt_in"`
	// NASScopes lists every NAS / service instance the account may authenticate
	// on (FR-64). An explicit empty array clears the scope back to "any NAS";
	// omitted (nil) leaves the existing set unchanged. The set is replaced
	// wholesale — there is no add-one/remove-one verb.
	NASScopes *[]radius.NASScope `json:"nas_scopes"`
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
	// ?service_type= keeps the subscriber list unified (C10): hotspot-only
	// accounts live in the one list and are found by filtering, not in a
	// separate section.
	serviceType := r.URL.Query().Get("service_type")
	switch serviceType {
	case "", "pppoe", "hotspot", "dual":
	default:
		httpapi.Error(w, http.StatusBadRequest, "invalid_filter", "service_type must be one of: pppoe hotspot dual")
		return
	}

	scope := auth.ScopeFilter(r.Context())
	// $1 cursor, $2 limit, $3 service_type filter, optional $4 owner scope.
	q := `SELECT ` + columns + ` FROM subscribers
	       WHERE ($1::uuid IS NULL OR id > $1::uuid)
	         AND ($3::text = '' OR service_type = $3::text)`
	args := []any{after, page.Limit + 1, serviceType}
	clause, arg := scopeClause(scope, 4)
	q += clause
	q += ` ORDER BY id LIMIT $2`
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
	// Attach after trimming to the page: the +1 lookahead row is discarded.
	if err := attachScopes(r.Context(), m.db, items); err != nil {
		m.internalError(w, "list nas scopes", err)
		return
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
	// service_type is NOT NULL; default to 'pppoe' (FR-61 / C9) when omitted, so
	// an existing API client that never learned the field keeps v1's meaning.
	serviceType := "pppoe"
	if in.ServiceType != nil && *in.ServiceType != "" {
		serviceType = *in.ServiceType
	}

	// The subscriber and its FR-64 scope set commit together: a subscriber that
	// landed without its scopes would authenticate on every NAS, which is the
	// widening this feature exists to prevent.
	tx, err := m.db.Begin(r.Context())
	if err != nil {
		m.internalError(w, "begin create", err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	s, err := scanSubscriber(tx.QueryRow(r.Context(),
		`INSERT INTO subscribers
		   (username, password_enc, name, phone, address, notes, status, profile_id,
		    owner_manager_id, expires_at, mac_lock_mode, static_ip, session_limit_override,
		    rate_override, price_override, disabled_reason, service_type, whatsapp_opt_in,
		    has_password, quota_cycle_anchor)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8::uuid,$9::uuid,$10,$11,$12::inet,$13,$14,$15,$16,$17,$18,$19, now())
		 RETURNING `+columns,
		in.Username, enc, in.Name, norm.phonePtr, in.Address, in.Notes, status, in.ProfileID,
		owner, in.ExpiresAt, macMode, norm.staticIP, in.SessionLimitOverride,
		in.RateOverride, in.PriceOverride, in.DisabledReason, serviceType,
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
	if err := radius.ReplaceScopes(r.Context(), tx, radius.SubscriberScopes, s.ID, norm.nasScopes); err != nil {
		m.internalError(w, "create nas scopes", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		m.internalError(w, "commit create", err)
		return
	}
	s.NASScopes = orEmptyScopes(norm.nasScopes)
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

	// Row + scope set commit together (see createHandler).
	tx, err := m.db.Begin(r.Context())
	if err != nil {
		m.internalError(w, "begin update", err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	after, err := scanSubscriber(tx.QueryRow(r.Context(),
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
		    service_type          = COALESCE($29, service_type),
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
		nilIfEmpty(in.ServiceType), in.WhatsappOptIn,
		hasPasswordArg))
	if err != nil {
		if isForeignKeyViolation(err) {
			httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
				httpapi.FieldError{Field: "profile_id", Message: "unknown profile"})
			return
		}
		m.internalError(w, "update", err)
		return
	}
	// A request that never mentioned nas_scopes leaves the set alone; one that
	// sent an empty array clears it to "any NAS".
	if norm.writeNASScope {
		if err := radius.ReplaceScopes(r.Context(), tx, radius.SubscriberScopes, id, norm.nasScopes); err != nil {
			m.internalError(w, "update nas scopes", err)
			return
		}
		after.NASScopes = orEmptyScopes(norm.nasScopes)
	} else {
		after.NASScopes = before.NASScopes
	}
	if err := tx.Commit(r.Context()); err != nil {
		m.internalError(w, "commit update", err)
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
	// Refuse rather than orphan their ledger rows — see hasFinancialHistory.
	paid, err := hasFinancialHistory(r.Context(), m.db, id)
	if err != nil {
		m.internalError(w, "check financial history", err)
		return
	}
	if paid {
		httpapi.Error(w, http.StatusConflict, "has_billing_history",
			"this subscriber has billing history and cannot be deleted; disable them instead so their payments stay attached to them")
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
	// nasScopes is the validated FR-64 scope set; writeNASScope reports whether
	// the request touched it at all (false = leave the existing set alone on
	// update). An empty set with writeNASScope true clears to "any NAS".
	nasScopes     []radius.NASScope
	writeNASScope bool
}

// normalizeWrite validates and canonicalizes the cross-cutting fields shared by
// create and update: phone (FR-1.3), static IP (FR-16.2), mac_lock_mode enum,
// status enum, service_type enum (FR-61), the NAS scope pair (FR-64), and the
// whatsapp-opt-in→phone dependency (FR-1.5). excludeID is the subscriber being
// updated ("" on create) for static-IP uniqueness.
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

	if in.ServiceType != nil && *in.ServiceType != "" {
		switch *in.ServiceType {
		case "pppoe", "hotspot", "dual":
		default:
			add("service_type", "must be one of: pppoe hotspot dual")
		}
	}

	if nasFE, err := m.normalizeNASScopes(ctx, in.NASScopes, &out); err != nil {
		m.log.Error("subscribers: nas scope check", "error", err)
		add("nas_scopes", "could not validate the NAS scope")
	} else {
		fe = append(fe, nasFE...)
	}

	if in.OwnerManagerID != nil && *in.OwnerManagerID != "" {
		out.owner = in.OwnerManagerID
	}
	return out, fe
}

// normalizeNASScopes validates the FR-64 scope set (C4). A nil set means the
// request did not mention scopes at all, so an update leaves the existing set
// alone; a present-but-empty array is an explicit "any NAS".
//
// Validation lives in the radius package because it is the same check for
// subscribers and profiles against tables neither owns. An error return is
// infrastructure failure, distinct from the per-entry messages.
func (m *Module) normalizeNASScopes(ctx context.Context, in *[]radius.NASScope, out *normalized) ([]httpapi.FieldError, error) {
	if in == nil {
		return nil, nil
	}
	out.writeNASScope = true
	out.nasScopes = radius.DedupeScopes(*in)

	bad, err := radius.ValidateScopes(ctx, m.db, out.nasScopes)
	if err != nil {
		return nil, err
	}
	fe := make([]httpapi.FieldError, 0, len(bad))
	for i, msg := range bad {
		fe = append(fe, httpapi.FieldError{Field: fmt.Sprintf("nas_scopes.%d", i), Message: msg})
	}
	return fe, nil
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
