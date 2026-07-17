package push

// Subscription persistence (contract C1-C). One row per browser endpoint,
// owned by exactly one manager (panel) or subscriber (portal). endpoint is
// globally unique so a re-subscribe from the same device upserts in place
// (edge case: duplicate subscriptions per endpoint deduped).

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Subscription is one push_subscriptions row.
type Subscription struct {
	ID       string
	Surface  string // panel | portal
	OwnerID  string // manager_id or subscriber_id, per Surface
	Endpoint string
	P256dh   string
	Auth     string
}

// Keys is the browser PushSubscription.toJSON().keys shape.
type Keys struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

// upsert inserts or replaces the row for endpoint (dedup by endpoint).
func upsert(ctx context.Context, db *pgxpool.Pool, surface, ownerID, endpoint string, keys Keys) error {
	managerCol, subscriberCol := "NULL::uuid", "NULL::uuid"
	if surface == surfacePanel {
		managerCol = "$2::uuid"
	} else {
		subscriberCol = "$2::uuid"
	}
	_, err := db.Exec(ctx,
		`INSERT INTO push_subscriptions (surface, manager_id, subscriber_id, endpoint, p256dh, auth)
		 VALUES ($1, `+managerCol+`, `+subscriberCol+`, $3, $4, $5)
		 ON CONFLICT (endpoint) DO UPDATE SET
		   surface = EXCLUDED.surface, manager_id = EXCLUDED.manager_id,
		   subscriber_id = EXCLUDED.subscriber_id, p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth`,
		surface, ownerID, endpoint, keys.P256dh, keys.Auth)
	return err
}

// remove deletes the subscription for endpoint (unsubscribe), scoped to the
// caller's own surface+owner so one manager/subscriber cannot prune another's.
func remove(ctx context.Context, db *pgxpool.Pool, surface, ownerID, endpoint string) error {
	col := "manager_id"
	if surface == surfacePortal {
		col = "subscriber_id"
	}
	_, err := db.Exec(ctx,
		`DELETE FROM push_subscriptions WHERE endpoint = $1 AND surface = $2 AND `+col+` = $3::uuid`,
		endpoint, surface, ownerID)
	return err
}

// prune deletes a subscription after the push service reports it gone (410
// Gone / 404 Not Found) regardless of owner — the endpoint itself is dead.
func prune(ctx context.Context, db *pgxpool.Pool, endpoint string) error {
	_, err := db.Exec(ctx, `DELETE FROM push_subscriptions WHERE endpoint = $1`, endpoint)
	return err
}

// listBySurface returns every subscription for a surface (panel alert fan-out).
func listBySurface(ctx context.Context, db *pgxpool.Pool, surface string) ([]Subscription, error) {
	rows, err := db.Query(ctx,
		`SELECT id::text, surface, COALESCE(manager_id::text, subscriber_id::text), endpoint, p256dh, auth
		   FROM push_subscriptions WHERE surface = $1`, surface)
	if err != nil {
		if isUndefinedTable(err) {
			return nil, nil
		}
		return nil, err
	}
	return scanSubs(rows)
}

// listForSubscriber returns a portal subscriber's own subscriptions (expiry
// reminder targeting, task 4).
func listForSubscriber(ctx context.Context, db *pgxpool.Pool, subscriberID string) ([]Subscription, error) {
	rows, err := db.Query(ctx,
		`SELECT id::text, surface, subscriber_id::text, endpoint, p256dh, auth
		   FROM push_subscriptions WHERE surface = $1 AND subscriber_id = $2::uuid`,
		surfacePortal, subscriberID)
	if err != nil {
		if isUndefinedTable(err) {
			return nil, nil
		}
		return nil, err
	}
	return scanSubs(rows)
}

// listForManager returns one manager's own panel subscriptions (v2-2, FR-80.2:
// targeting the OWNING manager specifically, unlike DeliverPanel's broadcast
// to every admin's alert subscriptions).
func listForManager(ctx context.Context, db *pgxpool.Pool, managerID string) ([]Subscription, error) {
	rows, err := db.Query(ctx,
		`SELECT id::text, surface, manager_id::text, endpoint, p256dh, auth
		   FROM push_subscriptions WHERE surface = $1 AND manager_id = $2::uuid`,
		surfacePanel, managerID)
	if err != nil {
		if isUndefinedTable(err) {
			return nil, nil
		}
		return nil, err
	}
	return scanSubs(rows)
}

func scanSubs(rows pgx.Rows) ([]Subscription, error) {
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(&s.ID, &s.Surface, &s.OwnerID, &s.Endpoint, &s.P256dh, &s.Auth); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// isUndefinedTable degrades a read to empty when the migration hasn't applied
// yet, mirroring monitorsvc's helper of the same name (concurrent-agent boot
// window, not expected once 0330 has run).
func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42P01" || pgErr.Code == "42703"
	}
	return false
}
