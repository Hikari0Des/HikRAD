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
	"github.com/hikrad/hikrad/internal/radius"
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
	// NASScopes lists every NAS / service instance the account may authenticate
	// on (FR-64), enforced at auth. EMPTY = any NAS, the v1 behaviour and the
	// default. Never nil in a response: an empty JSON array says "anywhere"
	// unambiguously, where null invites a client to read it as "nowhere".
	NASScopes        []radius.NASScope `json:"nas_scopes"`
	PendingProfileID *string           `json:"pending_profile_id"`
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
	service_type, whatsapp_opt_in,
	pending_profile_id::text, has_password, created_at, updated_at`

func scanSubscriber(row pgx.Row) (Subscriber, error) {
	var s Subscriber
	err := row.Scan(&s.ID, &s.Username, &s.Name, &s.Phone, &s.Address, &s.Notes, &s.Status,
		&s.ProfileID, &s.OwnerManagerID, &s.ExpiresAt, &s.MacLockMode, &s.LearnedMac,
		&s.StaticIP, &s.SessionLimitOverride, &s.RateOverride, &s.PriceOverride, &s.DisabledReason,
		&s.ServiceType, &s.WhatsappOptIn,
		&s.PendingProfileID, &s.HasPassword, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return Subscriber{}, err
	}
	// Scopes live in their own table and are attached by the caller; default to
	// an empty set rather than nil so a row that is never enriched still
	// serializes as "any NAS" instead of null.
	s.NASScopes = []radius.NASScope{}
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
	s, err := scanSubscriber(db.QueryRow(ctx,
		`SELECT `+columns+` FROM subscribers WHERE id = $1::uuid`+clause, args...))
	if err != nil {
		return Subscriber{}, err
	}
	scopes, err := radius.LoadScopes(ctx, db, radius.SubscriberScopes, s.ID)
	if err != nil {
		return Subscriber{}, err
	}
	s.NASScopes = orEmptyScopes(scopes)
	return s, nil
}

// orEmptyScopes normalizes nil to an empty set. The panel reads "no scopes" as
// "any NAS" (FR-64's default), and null would leave that to a client's guess.
func orEmptyScopes(in []radius.NASScope) []radius.NASScope {
	if in == nil {
		return []radius.NASScope{}
	}
	return in
}

// attachScopes fills the NASScopes of a whole page in one query rather than one
// per row — the list endpoints are the hot read path for a panel operator.
func attachScopes(ctx context.Context, db *pgxpool.Pool, items []Subscriber) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	for _, s := range items {
		ids = append(ids, s.ID)
	}
	byOwner, err := radius.LoadScopesFor(ctx, db, radius.SubscriberScopes, ids)
	if err != nil {
		return err
	}
	for i := range items {
		items[i].NASScopes = orEmptyScopes(byOwner[items[i].ID])
	}
	return nil
}

// errHasFinancialHistory is why a subscriber cannot be deleted (FR-1 delete /
// FR-19 money invariants).
var errHasFinancialHistory = errors.New("subscriber has billing history")

// hasFinancialHistory reports whether a subscriber has ever transacted.
//
// Deleting one who has is refused, and the reason is the ledger, not caution:
// `ledger_transactions.subscriber_id` and `payments.subscriber_id` are ON DELETE
// SET NULL, so the money rows would survive with no owner — revenue totals stay
// correct while the customer they came from silently disappears from every
// per-subscriber report and receipt. `card_payments` / `payment_intents` are NO
// ACTION and would instead fail with a raw FK error the operator cannot read.
// Neither is an acceptable answer to "remove this subscriber", so the honest one
// is: a customer who has paid you is disabled, not deleted.
//
// A never-used account — the mis-typed row an operator actually wants gone — has
// no such rows and deletes cleanly.
func hasFinancialHistory(ctx context.Context, db *pgxpool.Pool, id string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM ledger_transactions WHERE subscriber_id = $1::uuid)
		     OR EXISTS (SELECT 1 FROM payments WHERE subscriber_id = $1::uuid)
		     OR EXISTS (SELECT 1 FROM card_payments WHERE subscriber_id = $1::uuid)
		     OR EXISTS (SELECT 1 FROM payment_intents WHERE subscriber_id = $1::uuid)`,
		id).Scan(&exists)
	return exists, err
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
