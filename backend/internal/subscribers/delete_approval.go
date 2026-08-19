package subscribers

// FR-159: subscriber delete is a money-returning-adjacent, destructive
// action (the phase brief names it alongside undo/invoice-void) — a scoped
// agent's request parks for a manager holding the approvals permission
// instead of executing immediately. In practice deleteHandler already
// refuses to delete any subscriber with billing history (hasFinancialHistory
// below), so there is rarely real money to reverse here; the gate exists as
// the same destructive-action safeguard the brief asks for regardless.
//
// executeSubscriberDelete is registered as approvals' ONE executor for this
// kind (FR-159's frozen contract: no separate "approved" implementation) —
// deleteHandler calls it directly for an unscoped caller via
// approvals.RequestOrExecute, and approvals.Approve calls it again, verbatim,
// on approval.

import (
	"context"
	"encoding/json"

	"github.com/hikrad/hikrad/internal/approvals"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/radius"
	"github.com/jackc/pgx/v5/pgxpool"
)

const approvalKindSubscriberDelete = "subscriber_delete"

// subscriberDeletePayload carries the ORIGINAL (possibly scoped) requester's
// identity, not the approver's — replaying the exact same authorization
// context the requester had is what makes "approval replays the original
// code path" mean something: an approver with broader access must never be
// able to delete a subscriber the original agent could not have.
type subscriberDeletePayload struct {
	SubscriberID       string `json:"subscriber_id"`
	RequesterManagerID string `json:"requester_manager_id"`
	RequesterScoped    bool   `json:"requester_scoped"`
}

func init() {
	approvals.Register(approvalKindSubscriberDelete, executeSubscriberDelete)
}

func executeSubscriberDelete(ctx context.Context, db *pgxpool.Pool, raw json.RawMessage, _ string) (any, error) {
	var p subscriberDeletePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	var scope *auth.ManagerScope
	if p.RequesterScoped {
		scope = &auth.ManagerScope{ManagerID: p.RequesterManagerID}
	}

	before, err := getByID(ctx, db, p.SubscriberID, scope)
	if err != nil {
		return nil, err
	}
	paid, err := hasFinancialHistory(ctx, db, p.SubscriberID)
	if err != nil {
		return nil, err
	}
	if paid {
		return nil, errHasFinancialHistory
	}
	if err := deleteByID(ctx, db, p.SubscriberID, scope); err != nil {
		return nil, err
	}
	_ = auth.Audit(ctx, "subscriber.delete", "subscriber", p.SubscriberID, before, nil)
	_ = radius.InvalidatePolicy(p.SubscriberID)
	return map[string]any{"deleted": true}, nil
}
