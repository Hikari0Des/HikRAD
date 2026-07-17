package billing

// Pure unit tests — no DB/Redis, always run in CI. They cover the money-math and
// rendering the DB-backed suite would otherwise re-derive: the anchor-rule matrix
// (FR-19.1 / AC-19a), price resolution (FR-19.2), refund rollback (FR-25),
// voucher code entropy/format (FR-22.1), and receipt rendering in both locales.

import (
	"strings"
	"testing"
	"time"
)

func ptime(s string) *time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return &t
}

// TestAnchorMatrix exercises active/expired × from_expiry/from_now (AC-19a).
func TestAnchorMatrix(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	future := ptime("2026-07-14T12:00:00Z") // active, 3 days left
	past := ptime("2026-07-01T12:00:00Z")   // already expired

	cases := []struct {
		name    string
		current *time.Time
		anchor  string
		days    int
		want    time.Time
	}{
		{"active+from_expiry extends from expiry", future, anchorFromExpiry, 30, future.AddDate(0, 0, 30)},
		{"active+from_now extends from now", future, anchorFromNow, 30, now.AddDate(0, 0, 30)},
		{"expired+from_expiry extends from now", past, anchorFromExpiry, 30, now.AddDate(0, 0, 30)},
		{"expired+from_now extends from now", past, anchorFromNow, 30, now.AddDate(0, 0, 30)},
		{"never-expired extends from now", nil, anchorFromExpiry, 30, now.AddDate(0, 0, 30)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := computeExpiry(now, c.current, c.anchor, c.days)
			if !got.Equal(c.want) {
				t.Fatalf("computeExpiry = %v, want %v", got, c.want)
			}
		})
	}
}

func TestResolvePrice(t *testing.T) {
	if got := resolvePrice(nil, 10000); got != 10000 {
		t.Fatalf("no override = %d, want 10000", got)
	}
	ov := int64(7500)
	if got := resolvePrice(&ov, 10000); got != 7500 {
		t.Fatalf("override = %d, want 7500", got)
	}
}

func TestRollbackExpiry(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	// Renewal added 30 days onto an active account: rollback removes them.
	cur := ptime("2026-08-10T12:00:00Z")
	if got := rollbackExpiry(now, cur, 30); !got.Equal(now.AddDate(0, 0, -30).AddDate(0, 0, 30)) {
		// cur - 30 days = 2026-07-11 == now; floor keeps it at now.
	}
	got := rollbackExpiry(now, cur, 30)
	if !got.Equal(*ptime("2026-07-11T12:00:00Z")) {
		t.Fatalf("rollback = %v, want now", got)
	}
	// Refunding a renewal on an already-expired-in-future account floors at now.
	near := ptime("2026-07-12T12:00:00Z")
	if got := rollbackExpiry(now, near, 30); !got.Equal(now) {
		t.Fatalf("rollback floor = %v, want now", got)
	}
	// Nil current floors at now.
	if got := rollbackExpiry(now, nil, 30); !got.Equal(now) {
		t.Fatalf("rollback nil = %v, want now", got)
	}
}

func TestRateString(t *testing.T) {
	if got := rateString(10240, 20480); got != "10M/20M" {
		t.Fatalf("rateString = %q, want 10M/20M", got)
	}
	if got := rateString(0, 0); got != "" {
		t.Fatalf("rateString zero = %q, want empty", got)
	}
	ov := "2M/8M"
	if got := resolveRate(&ov, 10240, 20480); got != "2M/8M" {
		t.Fatalf("resolveRate override = %q, want 2M/8M", got)
	}
}

// TestVoucherCodeFormat asserts entropy/format guarantees (FR-22.1): unambiguous
// alphabet, minimum length, prefix honored, high uniqueness.
func TestVoucherCodeFormat(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 5000; i++ {
		c := genCode("NET", 0)
		if !strings.HasPrefix(c, "NET") {
			t.Fatalf("code %q missing prefix", c)
		}
		if len(c) < minCodeLen {
			t.Fatalf("code %q shorter than %d", c, minCodeLen)
		}
		for _, r := range c[len("NET"):] {
			if !strings.ContainsRune(voucherAlphabet, r) {
				t.Fatalf("code %q has ambiguous/invalid char %q", c, r)
			}
		}
		if _, dup := seen[c]; dup {
			t.Fatalf("duplicate code %q within 5000", c)
		}
		seen[c] = struct{}{}
	}
	// Ambiguous characters must never appear in the alphabet.
	for _, bad := range "01OIl" {
		if strings.ContainsRune(voucherAlphabet, bad) {
			t.Fatalf("alphabet contains ambiguous %q", bad)
		}
	}
	// A long prefix still yields >= 8 random chars.
	if got := randomPartLen("VERYLONGPREFIX", 0); got != 8 {
		t.Fatalf("randomPartLen long = %d, want 8", got)
	}
	// A requested total length (item 20) is honored exactly when the prefix
	// leaves >= 8 random chars.
	if c := genCode("NET", 16); len(c) != 16 {
		t.Fatalf("genCode(NET, 16) length = %d, want 16", len(c))
	}
	if c := genCode("", 12); len(c) != 12 {
		t.Fatalf("genCode(\"\", 12) length = %d, want 12", len(c))
	}
	// The 8-char entropy floor still wins over a too-tight request.
	if c := genCode("VERYLONGPREFIX", 10); len(c) != len("VERYLONGPREFIX")+8 {
		t.Fatalf("genCode(VERYLONGPREFIX, 10) length = %d, want %d", len(c), len("VERYLONGPREFIX")+8)
	}
}

func TestHashCodeStableAndCaseInsensitive(t *testing.T) {
	if hashCode("net-abc") != hashCode("NET-ABC") {
		t.Fatal("hashCode should be case-insensitive")
	}
	if hashCode(" net-abc ") != hashCode("NET-ABC") {
		t.Fatal("hashCode should trim whitespace")
	}
	if hashCode("a") == hashCode("b") {
		t.Fatal("distinct codes must hash differently")
	}
}

// Printed vouchers are grouped ("ABCD-1234", "ABCD 1234") and subscribers type
// them exactly as printed — separators must never change the hash.
func TestHashCodeIgnoresSeparators(t *testing.T) {
	want := hashCode("NETABCD1234")
	for _, in := range []string{"NET-ABCD-1234", "net abcd 1234", " NET.ABCD.1234 ", "NET_ABCD_1234"} {
		if hashCode(in) != want {
			t.Fatalf("hashCode(%q) should equal hashCode(NETABCD1234)", in)
		}
	}
	if normalizeCode(" ab-cd 12.34_ef ") != "ABCD1234EF" {
		t.Fatalf("normalizeCode = %q, want ABCD1234EF", normalizeCode(" ab-cd 12.34_ef "))
	}
	// Legacy at-rest hashes (pre-normalization) keep separators; the redeem
	// path must therefore probe both forms.
	if legacyHashCode("NET-ABC") == hashCode("NET-ABC") {
		t.Fatal("legacy hash of a separator-bearing code must differ from the normalized hash")
	}
	if legacyHashCode("NETABC") != hashCode("NETABC") {
		t.Fatal("legacy and normalized hashes must agree on separator-free codes")
	}
}

func TestEasternArabicNumerals(t *testing.T) {
	if got := toEasternArabic("2026-07"); got != "٢٠٢٦-٠٧" {
		t.Fatalf("toEasternArabic = %q", got)
	}
}

func TestFormatThousands(t *testing.T) {
	for in, want := range map[int64]string{0: "0", 500: "500", 25000: "25,000", 1234567: "1,234,567", -25000: "-25,000"} {
		if got := formatThousands(in); got != want {
			t.Fatalf("formatThousands(%d) = %q, want %q", in, got, want)
		}
	}
}

// TestReceiptRendersBothLocales checks the receipt is RTL + Eastern-Arabic for ar
// and LTR + Latin for en, with the amount and receipt number present (FR-21).
func TestReceiptRendersBothLocales(t *testing.T) {
	d := receiptData{ReceiptNo: "HR-000042", Amount: 25000, Currency: "IQD", Method: "renewal",
		At: time.Date(2026, 7, 11, 9, 30, 0, 0, time.UTC), Subscriber: "Noor", Profile: "Home-20M"}
	brand := brandingConfig{Name: "AlNoor ISP", Phone: "0770"}

	en := renderReceipt(d, "en", "auto", brand)
	if !strings.Contains(en, `dir="ltr"`) || !strings.Contains(en, "25,000 IQD") || !strings.Contains(en, "HR-000042") {
		t.Fatalf("en receipt missing expected content:\n%s", en)
	}
	if !strings.Contains(en, "Receipt No") {
		t.Fatal("en receipt missing localized label")
	}

	ar := renderReceipt(d, "ar", "auto", brand)
	if !strings.Contains(ar, `dir="rtl"`) || !strings.Contains(ar, "٢٥") || !strings.Contains(ar, "إيصال") {
		t.Fatalf("ar receipt missing RTL/Eastern-Arabic/label:\n%s", ar)
	}
	// Latin override on an RTL locale keeps Latin digits.
	arLatin := renderReceipt(d, "ar", "latin", brand)
	if !strings.Contains(arLatin, "25,000") {
		t.Fatal("ar receipt with latin numerals should keep Latin digits")
	}
}

// TestReceiptRendersNonIQDCurrency (v2 phase 4): a USD payment's receipt
// shows USD, never a settings-level default currency.
func TestReceiptRendersNonIQDCurrency(t *testing.T) {
	d := receiptData{ReceiptNo: "HR-000043", Amount: 2500, Currency: "USD", Method: "renewal",
		At: time.Date(2026, 7, 11, 9, 30, 0, 0, time.UTC), Subscriber: "Noor", Profile: "Home-USD"}
	brand := brandingConfig{Name: "AlNoor ISP"}
	en := renderReceipt(d, "en", "auto", brand)
	if !strings.Contains(en, "2,500 USD") {
		t.Fatalf("expected the receipt to show USD, got:\n%s", en)
	}
}

func TestRedeemErrorMapping(t *testing.T) {
	if _, _, bad := redeemErrorFor(redeemOK); bad {
		t.Fatal("redeemOK should not be an error")
	}
	for _, o := range []redeemOutcome{redeemInvalid, redeemUsed, redeemExpired, redeemBatchVoid} {
		if _, _, bad := redeemErrorFor(o); !bad {
			t.Fatalf("outcome %d should map to an error", o)
		}
	}
}
