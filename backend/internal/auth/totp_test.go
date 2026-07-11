package auth

import (
	"strings"
	"testing"
	"time"
)

func TestTOTPRoundTrip(t *testing.T) {
	secret, err := generateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	code, err := totpCodeAt(secret, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != totpDigits {
		t.Fatalf("code %q not %d digits", code, totpDigits)
	}
	if !verifyTOTP(secret, code, now) {
		t.Fatal("valid code did not verify at its own time")
	}
	if verifyTOTP(secret, "000000", now) && code != "000000" {
		t.Fatal("wrong code verified")
	}
}

func TestTOTPClockSkew(t *testing.T) {
	secret, _ := generateTOTPSecret()
	now := time.Unix(1_700_000_000, 0)
	prev := now.Add(-totpPeriod) // one window back
	next := now.Add(totpPeriod)  // one window forward
	far := now.Add(3 * totpPeriod)

	prevCode, _ := totpCodeAt(secret, prev)
	nextCode, _ := totpCodeAt(secret, next)
	farCode, _ := totpCodeAt(secret, far)

	if !verifyTOTP(secret, prevCode, now) {
		t.Error("previous-window code should verify within ±1 skew")
	}
	if !verifyTOTP(secret, nextCode, now) {
		t.Error("next-window code should verify within ±1 skew")
	}
	if farCode != prevCode && farCode != nextCode && verifyTOTP(secret, farCode, now) {
		t.Error("code 3 windows away must not verify")
	}
}

func TestBackupCodeHashNormalization(t *testing.T) {
	// Case and dashes are normalized away before hashing.
	a := hashBackupCode("abcde-12345")
	b := hashBackupCode("ABCDE12345")
	c := hashBackupCode(" ABCDE-12345 ")
	if string(a) != string(b) || string(a) != string(c) {
		t.Fatal("backup code hashing must normalize case/dashes/space")
	}
	if string(a) == string(hashBackupCode("abcde-12346")) {
		t.Fatal("different codes must hash differently")
	}
}

func TestGenerateBackupCodes(t *testing.T) {
	codes, err := generateBackupCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != backupCodeCount {
		t.Fatalf("got %d codes, want %d", len(codes), backupCodeCount)
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if !strings.Contains(c, "-") {
			t.Fatalf("code %q missing separator", c)
		}
		if seen[c] {
			t.Fatalf("duplicate backup code %q", c)
		}
		seen[c] = true
	}
}

func TestOtpauthURI(t *testing.T) {
	uri := otpauthURI("sara", "JBSWY3DPEHPK3PXP")
	for _, want := range []string{"otpauth://totp/", "secret=JBSWY3DPEHPK3PXP", "issuer=HikRAD"} {
		if !strings.Contains(uri, want) {
			t.Errorf("uri %q missing %q", uri, want)
		}
	}
}
