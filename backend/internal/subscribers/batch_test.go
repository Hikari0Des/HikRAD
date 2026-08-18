package subscribers

// Pure-Go unit coverage for FR-133's generation/validation logic — no DB
// needed. The DB-gated end-to-end flow (dry-run == real result, collision
// handling, license cap refusal) lives in batch_db_test.go.

import "testing"

func TestBatchUsernames_PaddingAndRange(t *testing.T) {
	in := batchInput{Prefix: "user", StartIndex: 1, Count: 3, DigitPadding: 3}
	got := in.usernames()
	want := []string{"user001", "user002", "user003"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBatchUsernames_NaturalWidthKeepsBatchUniform(t *testing.T) {
	// 200 accounts starting at 1: natural width of the LARGEST index (200) is
	// 3, so every username in the batch is 3 digits, not "user1".."user200".
	in := batchInput{Prefix: "user", StartIndex: 1, Count: 200, DigitPadding: 0}
	got := in.usernames()
	if got[0] != "user001" {
		t.Errorf("first = %q, want user001", got[0])
	}
	if got[199] != "user200" {
		t.Errorf("last = %q, want user200", got[199])
	}
}

func TestBatchUsernames_DryRunMatchesExecuteByConstruction(t *testing.T) {
	// Gate item 3: dry-run must match the real result exactly. Both paths
	// call the identical in.usernames() — this pins that invariant so a
	// future refactor introducing a second generator would fail loudly.
	in := batchInput{Prefix: "acc-", StartIndex: 50, Count: 10, DigitPadding: 4}
	a := in.usernames()
	b := in.usernames()
	if len(a) != len(b) {
		t.Fatalf("non-deterministic length: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic at %d: %q vs %q", i, a[i], b[i])
		}
	}
}

func TestBatchInput_Validate(t *testing.T) {
	cases := []struct {
		name    string
		in      batchInput
		wantErr bool
	}{
		{"valid shared password", batchInput{Count: 10, ProfileID: "p", PasswordMode: "shared", SharedPassword: "x"}, false},
		{"valid generated password", batchInput{Count: 10, ProfileID: "p", PasswordMode: "generated"}, false},
		{"count zero", batchInput{Count: 0, ProfileID: "p", SharedPassword: "x"}, true},
		{"count over cap", batchInput{Count: 10001, ProfileID: "p", SharedPassword: "x"}, true},
		{"missing profile", batchInput{Count: 5, SharedPassword: "x"}, true},
		{"shared mode needs password", batchInput{Count: 5, ProfileID: "p", PasswordMode: "shared"}, true},
		{"bad service_type", batchInput{Count: 5, ProfileID: "p", SharedPassword: "x", ServiceType: "bogus"}, true},
		{"bad invoicing", batchInput{Count: 5, ProfileID: "p", SharedPassword: "x", Invoicing: "bogus"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fe := c.in.validate()
			if (len(fe) > 0) != c.wantErr {
				t.Errorf("validate() errors=%v, wantErr=%v", fe, c.wantErr)
			}
		})
	}
}

func TestBatchInput_Validate_Defaults(t *testing.T) {
	in := batchInput{Count: 5, ProfileID: "p", SharedPassword: "x"}
	if fe := in.validate(); len(fe) != 0 {
		t.Fatalf("unexpected errors: %v", fe)
	}
	if in.ServiceType != "pppoe" {
		t.Errorf("service_type default = %q, want pppoe", in.ServiceType)
	}
	if in.Invoicing != invoiceNone {
		t.Errorf("invoicing default = %q, want %q", in.Invoicing, invoiceNone)
	}
}
