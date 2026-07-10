package auth

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestArgon2idRoundTrip(t *testing.T) {
	h, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$") {
		t.Fatalf("not argon2id: %s", h)
	}
	ok, upgrade, err := verifyPassword(h, "correct horse battery staple")
	if err != nil || !ok {
		t.Fatalf("verify: ok=%v upgrade=%v err=%v", ok, upgrade, err)
	}
	if upgrade {
		t.Fatal("fresh argon2id hash must not need upgrade")
	}
	ok, _, _ = verifyPassword(h, "wrong")
	if ok {
		t.Fatal("wrong password verified")
	}
}

// The seed writes bcrypt; verifying a correct bcrypt password must succeed AND
// signal an upgrade to argon2id (the login upgrade path).
func TestBcryptVerifyFlagsUpgrade(t *testing.T) {
	bh, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.MinCost)
	ok, upgrade, err := verifyPassword(string(bh), "admin")
	if err != nil || !ok {
		t.Fatalf("bcrypt verify: ok=%v err=%v", ok, err)
	}
	if !upgrade {
		t.Fatal("correct bcrypt password must flag needsUpgrade")
	}
	ok, _, err = verifyPassword(string(bh), "wrong")
	if err != nil {
		t.Fatalf("bcrypt wrong password errored: %v", err)
	}
	if ok {
		t.Fatal("wrong bcrypt password verified")
	}
}

func TestUnknownHashFormat(t *testing.T) {
	if _, _, err := verifyPassword("not-a-hash", "x"); err == nil {
		t.Fatal("expected error for unknown hash format")
	}
}

func TestArgon2idTamperedFieldsRejected(t *testing.T) {
	h, _ := hashPassword("pw")
	// Corrupt the base64 hash segment.
	parts := strings.Split(h, "$")
	parts[5] = "!!!notbase64!!!"
	bad := strings.Join(parts, "$")
	if _, _, err := verifyPassword(bad, "pw"); err == nil {
		t.Fatal("expected error for corrupt argon2 hash")
	}
}
