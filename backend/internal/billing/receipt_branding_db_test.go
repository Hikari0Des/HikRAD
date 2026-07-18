package billing_test

// DB-backed regression test for the receipt header's branding bug (v2 phase
// 11, FR-91/FR-93): billing.receipt_branding used to be read as a
// {name,address,phone} object, but the only UI that ever wrote it
// (BillingSettings.tsx) sent a plain boolean — the unmarshal silently failed
// every time and every receipt showed the hardcoded "HikRAD" fallback
// regardless of what was configured. See docs/ops/known-issues.md.

import (
	"bytes"
	"net/http"
	"testing"
)

// putSettings writes a settings group through the real HTTP endpoint (the
// same platform.Settings instance every module — including billing's own
// showReceiptBranding — was constructed with, via httpapi.Deps.Settings).
// A second, independent platform.NewSettings(e.db) instance would write to
// the same DB but warm its OWN cache, leaving the server's already-cached
// reads stale — not a bug in the settings service (each process legitimately
// owns its own cache), just the wrong tool for asserting a same-process
// read-after-write.
func (e env) putSettings(t *testing.T, group string, body map[string]any) {
	t.Helper()
	r := e.do(t, "PUT", "/api/v1/settings/"+group, e.token, body)
	if r.status != http.StatusOK {
		t.Fatalf("PUT /api/v1/settings/%s = %d: %s", group, r.status, r.body)
	}
}

func TestReceiptBrandingBooleanRespected(t *testing.T) {
	e := setup(t)
	e.putSettings(t, "branding", map[string]any{
		"name": "AlNoor ISP", "primary_color": "#08748f", "secondary_color": "#0f172a",
	})

	prof := e.createProfile(t, 15000, 30)
	sub := e.createSubscriber(t, prof)

	// receipt_branding=true (the documented default): the receipt shows the
	// configured instance name.
	e.putSettings(t, "billing", map[string]any{
		"renewal_anchor": "from_expiry", "admin_balance_bypass": true, "receipt_prefix": "HR-",
		"receipt_branding": true, "voucher_prefix": "", "receipt_numerals": "auto",
	})
	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", e.token, map[string]any{})
	if r.status != http.StatusOK {
		t.Fatalf("renew (branding on) = %d: %s", r.status, r.body)
	}
	var out struct {
		ReceiptNo string `json:"receipt_no"`
	}
	r.into(t, &out)
	rec := e.do(t, "GET", "/api/v1/payments/"+out.ReceiptNo+"/receipt", e.token, nil)
	if rec.status != http.StatusOK {
		t.Fatalf("receipt render = %d: %s", rec.status, rec.body)
	}
	if !bytes.Contains(rec.body, []byte("AlNoor ISP")) {
		t.Errorf("receipt with receipt_branding=true should show the configured name; got:\n%s", rec.body)
	}

	// receipt_branding=false: the receipt shows the generic fallback,
	// regardless of what's configured.
	e.putSettings(t, "billing", map[string]any{
		"renewal_anchor": "from_expiry", "admin_balance_bypass": true, "receipt_prefix": "HR-",
		"receipt_branding": false, "voucher_prefix": "", "receipt_numerals": "auto",
	})
	r2 := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", e.token, map[string]any{})
	if r2.status != http.StatusOK {
		t.Fatalf("renew (branding off) = %d: %s", r2.status, r2.body)
	}
	var out2 struct {
		ReceiptNo string `json:"receipt_no"`
	}
	r2.into(t, &out2)
	rec2 := e.do(t, "GET", "/api/v1/payments/"+out2.ReceiptNo+"/receipt", e.token, nil)
	if rec2.status != http.StatusOK {
		t.Fatalf("receipt render = %d: %s", rec2.status, rec2.body)
	}
	if bytes.Contains(rec2.body, []byte("AlNoor ISP")) {
		t.Errorf("receipt with receipt_branding=false must NOT show the configured name; got:\n%s", rec2.body)
	}
	if !bytes.Contains(rec2.body, []byte("HikRAD")) {
		t.Errorf("receipt with receipt_branding=false should fall back to the generic literal; got:\n%s", rec2.body)
	}
}
