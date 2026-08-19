package billing

// Pure unit tests for the journal substrate's money math (FR-153) — no
// DB/Redis, always run. The DB-gated suite (v4_9_db_test.go) proves the
// same logic end-to-end against a real Postgres; these prove the arithmetic
// in isolation, fast and unconditionally.

import (
	"testing"
	"time"
)

func TestSignedPair_PositiveAmount(t *testing.T) {
	lines := signedPair("DEBIT_ACCT", "CREDIT_ACCT", 500, "memo", "mgr-1")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
	if lines[0].AccountCode != "DEBIT_ACCT" || lines[0].Debit != 500 || lines[0].Credit != 0 {
		t.Errorf("line0 = %+v, want debit 500 on DEBIT_ACCT", lines[0])
	}
	if lines[1].AccountCode != "CREDIT_ACCT" || lines[1].Credit != 500 || lines[1].Debit != 0 {
		t.Errorf("line1 = %+v, want credit 500 on CREDIT_ACCT", lines[1])
	}
	var d, c int64
	for _, l := range lines {
		d += l.Debit
		c += l.Credit
	}
	if d != c {
		t.Errorf("unbalanced: debit %d != credit %d", d, c)
	}
}

// TestSignedPair_NegativeAmountSwapsSides proves a refund/undo (whose
// amount is the exact negation of the original) reverses direction
// automatically, with no special-casing — the property renew.go's refund
// path and undo.go's undoTopupCore both depend on.
func TestSignedPair_NegativeAmountSwapsSides(t *testing.T) {
	pos := signedPair(acctAgentWallet, acctRevenue, 300, "sale", "mgr-1")
	neg := signedPair(acctAgentWallet, acctRevenue, -300, "reversal", "mgr-1")

	if pos[0].AccountCode != neg[1].AccountCode || pos[0].Debit != neg[1].Credit {
		t.Errorf("reversal did not swap sides: pos=%+v neg=%+v", pos, neg)
	}
	if pos[1].AccountCode != neg[0].AccountCode || pos[1].Credit != neg[0].Debit {
		t.Errorf("reversal did not swap sides: pos=%+v neg=%+v", pos, neg)
	}
	var d, c int64
	for _, l := range neg {
		d += l.Debit
		c += l.Credit
	}
	if d != c {
		t.Errorf("reversal unbalanced: debit %d != credit %d", d, c)
	}
}

// TestSignedPair_TagsManagerOnlyOnWalletLeg proves the subledger dimension
// (migration 0617's manager_id column) lands on the agent-wallet line and
// nowhere else — a mistagged line would silently break the per-manager
// reconciliation test.
func TestSignedPair_TagsManagerOnlyOnWalletLeg(t *testing.T) {
	lines := signedPair(acctAgentWallet, acctRevenue, 100, "x", "mgr-42")
	for _, l := range lines {
		if l.AccountCode == acctAgentWallet && l.ManagerID != "mgr-42" {
			t.Errorf("wallet line not tagged: %+v", l)
		}
		if l.AccountCode == acctRevenue && l.ManagerID != "" {
			t.Errorf("revenue line should not be tagged: %+v", l)
		}
	}
}

func TestValidateBalanced_RefusesUnbalancedSum(t *testing.T) {
	err := validateBalanced("test", []jLine{
		{AccountCode: "A", Debit: 100},
		{AccountCode: "B", Credit: 99},
	})
	if err == nil {
		t.Fatal("want an error for debit 100 != credit 99")
	}
}

func TestValidateBalanced_RefusesFewerThanTwoLines(t *testing.T) {
	if err := validateBalanced("test", []jLine{{AccountCode: "A", Debit: 100}}); err == nil {
		t.Fatal("want an error for a single-line entry")
	}
	if err := validateBalanced("test", nil); err == nil {
		t.Fatal("want an error for zero lines")
	}
}

func TestValidateBalanced_RefusesBothSidesOnOneLine(t *testing.T) {
	err := validateBalanced("test", []jLine{
		{AccountCode: "A", Debit: 100, Credit: 100},
		{AccountCode: "B", Credit: 100},
	})
	if err == nil {
		t.Fatal("want an error for a line with both debit and credit set")
	}
}

func TestValidateBalanced_AcceptsBalancedPair(t *testing.T) {
	err := validateBalanced("test", []jLine{
		{AccountCode: "A", Debit: 500},
		{AccountCode: "B", Credit: 500},
	})
	if err != nil {
		t.Fatalf("unexpected error for a balanced pair: %v", err)
	}
}

func TestRecomputeInvoiceStatus(t *testing.T) {
	today := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	future := today.AddDate(0, 0, 10)
	past := today.AddDate(0, 0, -10)

	cases := []struct {
		name       string
		amountPaid int64
		total      int64
		dueDate    time.Time
		want       string
	}{
		{"unpaid, not due", 0, 1000, future, "unpaid"},
		{"partial, not due", 400, 1000, future, "partial"},
		{"fully paid, before due", 1000, 1000, future, "paid"},
		{"fully paid, after due (paid wins over due)", 1000, 1000, past, "paid"},
		{"unpaid, overdue", 0, 1000, past, "due"},
		{"partial, overdue (due wins over partial)", 400, 1000, past, "due"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := recomputeInvoiceStatus(tc.amountPaid, tc.total, tc.dueDate, today)
			if got != tc.want {
				t.Errorf("recomputeInvoiceStatus(%d,%d,...) = %q, want %q", tc.amountPaid, tc.total, got, tc.want)
			}
		})
	}
}

func TestFormatDocNo(t *testing.T) {
	if got := formatDocNo("INV-", 42); got != "INV-000042" {
		t.Errorf("formatDocNo = %q", got)
	}
}
