package subscribers

// Subscriber persistence (FR-1). The read shape never carries password_enc —
// passwords are reset-only and reversible-encrypted at rest (NFR-4.2). Manager
// scoping (FR-27.2) is applied by every query through scopeClause.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Subscriber is the full read shape (C7-D). Nullable columns are pointers so
// "unset" (inherit) round-trips distinctly from a zero value.
type Subscriber struct {
	ID                   string     `json:"id"`
	Username             string     `json:"username"`
	Name                 *string    `json:"name"`
	Phone                *string    `json:"phone"`
	Address              *string    `json:"address"`
	Notes                *string    `json:"notes"`
	Status               string     `json:"status"`
	ProfileID            *string    `json:"profile_id"`
	OwnerManagerID       *string    `json:"owner_manager_id"`
	ExpiresAt            *time.Time `json:"expires_at"`
	MacLockMode          string     `json:"mac_lock_mode"`
	LearnedMac           *string    `json:"learned_mac"`
	StaticIP             *string    `json:"static_ip"`
	SessionLimitOverride *int       `json:"session_limit_override"`
	RateOverride         *string    `json:"rate_override"`
	PriceOverride        *int64     `json:"price_override"`
	DisabledReason       *string    `json:"disabled_reason"`
	// ServiceType (FR-61) is pppoe | hotspot | dual — it replaced v1's
	// allow_hotspot bool, which could not express a hotspot-only account.
	ServiceType   string `json:"service_type"`
	WhatsappOptIn bool   `json:"whatsapp_opt_in"`
	// NASID/NASServiceID scope the account to one NAS / one service instance
	// (FR-64), enforced at auth. Both nil = any NAS, the v1 behaviour.
	NASID            *string `json:"nas_id"`
	NASServiceID     *string `json:"nas_service_id"`
	PendingProfileID *string `json:"pending_profile_id"`
	// HasPassword is false for passwordless hotspot logins (item 13) — the
	// credential column then seals an empty string and the portal refuses
	// password login for the account.
	HasPassword bool      `json:"has_password"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// columns is the SELECT list backing scanSubscriber. host(static_ip) strips the
// /32 an inet host address would otherwise render with.
const columns = `id::text, username::text, name, phone, address, notes, status,
	profile_id::text, owner_manager_id::text, expires_at, mac_lock_mode, learned_mac,
	host(static_ip), session_limit_override, rate_override, price_override, disabled_reason,
	service_type, whatsapp_opt_in, nas_id::text, nas_service_id::text,
	pending_profile_id::text, has_password, created_at, updated_at`

func scanSubscriber(row pgx.Row) (Subscriber, error) {
	var s Subscriber
	err := row.Scan(&s.ID, &s.Username, &s.Name, &s.Phone, &s.Address, &s.Notes, &s.Status,
		&s.ProfileID, &s.OwnerManagerID, &s.ExpiresAt, &s.MacLockMode, &s.LearnedMac,
		&s.StaticIP, &s.SessionLimitOverride, &s.RateOverride, &s.PriceOverride, &s.DisabledReason,
		&s.ServiceType, &s.WhatsappOptIn, &s.NASID, &s.NASServiceID,
		&s.PendingProfileID, &s.HasPassword, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return Subscriber{}, err
	}
	s.CreatedAt = s.CreatedAt.UTC()
	s.UpdatedAt = s.UpdatedAt.UTC()
	if s.ExpiresAt != nil {
		u := s.ExpiresAt.UTC()
		s.ExpiresAt = &u
	}
	return s, nil
}

// scopeClause appends `AND owner_manager_id = $N` for a scoped caller, so every
// list/get/mutation is filtered server-side (FR-27.2). It returns the SQL
// fragment and the argument to append (nil when unscoped). startArg is the next
// positional placeholder index.
func scopeClause(scope *auth.ManagerScope, startArg int) (string, any) {
	if scope == nil {
		return "", nil
	}
	return fmt.Sprintf(" AND owner_manager_id = $%d::uuid", startArg), scope.ManagerID
}

func getByID(ctx context.Context, db *pgxpool.Pool, id string, scope *auth.ManagerScope) (Subscriber, error) {
	clause, arg := scopeClause(scope, 2)
	args := []any{id}
	if arg != nil {
		args = append(args, arg)
	}
	return scanSubscriber(db.QueryRow(ctx,
		`SELECT `+columns+` FROM subscribers WHERE id = $1::uuid`+clause, args...))
}

// existsStaticIP reports whether any OTHER subscriber already holds ip (the
// per-subscriber uniqueness half of FR-16.2). excludeID may be "" on create.
func existsStaticIP(ctx context.Context, db *pgxpool.Pool, ip, excludeID string) (bool, error) {
	var n int
	err := db.QueryRow(ctx,
		`SELECT count(*) FROM subscribers
		  WHERE static_ip = $1::inet AND ($2 = '' OR id <> $2::uuid)`,
		ip, excludeID).Scan(&n)
	return n > 0, err
}

func deleteByID(ctx context.Context, db *pgxpool.Pool, id string, scope *auth.ManagerScope) error {
	clause, arg := scopeClause(scope, 2)
	args := []any{id}
	if arg != nil {
		args = append(args, arg)
	}
	ct, err := db.Exec(ctx, `DELETE FROM subscribers WHERE id = $1::uuid`+clause, args...)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// isUniqueViolation reports a 23505 (used to map a duplicate username to 409).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func isNotFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
