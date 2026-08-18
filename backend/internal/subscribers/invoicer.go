package subscribers

// Invoicer is billing's seam for FR-133 batch creation's per-batch invoicing
// choice ("invoice each" / "one invoice for the batch" / "invoice none").
// subscribers never imports billing directly, so it never owns money logic —
// same seam discipline radius.SetPolicyProvider/SetVoucherAuthenticator
// already establish for exactly this reason (avoiding an import cycle AND
// keeping the money invariants owned in one place). billing registers the
// concrete implementation at boot via SetInvoicer.

import "context"

// Invoicer runs the real billing paths a freshly created batch of subscribers
// needs, without subscribers ever touching a ledger row itself.
type Invoicer interface {
	// InvoiceSubscriber runs the normal FR-19 renewal for one newly created
	// subscriber: charges the profile price, extends expiry from now, and
	// activates the account. actorScoped mirrors the acting manager's
	// auth.Manager.Scoped (agents always enforce balance; admins per settings —
	// billing owns that policy, this only carries the fact needed to apply it).
	InvoiceSubscriber(ctx context.Context, subscriberID, actorManagerID string, actorScoped bool) error
	// InvoiceBatch posts ONE ledger entry covering count subscribers on
	// profileID — "one invoice for the whole batch" rather than N receipts.
	// The subscribers themselves are provisioned directly (status/expiry) by
	// the caller; this only moves the money.
	InvoiceBatch(ctx context.Context, profileID, actorManagerID string, actorScoped bool, count int, reference string) error
}

var invoicer Invoicer

// SetInvoicer installs billing's seam (called once at boot).
func SetInvoicer(i Invoicer) { invoicer = i }

func currentInvoicer() Invoicer { return invoicer }
