package platform

// FR-127: proves notification-channel credentials are genuinely encrypted
// at rest, not merely round-tripped through an API shape — the actual
// property this phase's encryption-at-rest audit item is checking for.

import (
	"bytes"
	"context"
	"testing"

	"github.com/hikrad/hikrad/internal/platform/crypto"
)

func init() {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 41)
	}
	_ = crypto.Configure(key)
}

type smtpFixture struct {
	Host     string `json:"host"`
	Password string `json:"password"`
}

func TestSetEncrypted_RawDBValueDoesNotContainPlaintext(t *testing.T) {
	db := newFakeDB()
	s := NewSettings(db)
	ctx := context.Background()

	secret := "correct-horse-battery-staple"
	if err := SetEncrypted(ctx, s, "notifications.smtp", smtpFixture{Host: "smtp.example.com", Password: secret}); err != nil {
		t.Fatalf("SetEncrypted: %v", err)
	}

	db.mu.Lock()
	rawStored := db.store["notifications.smtp"]
	db.mu.Unlock()
	if rawStored == nil {
		t.Fatal("nothing was written to the DB for notifications.smtp")
	}
	if bytes.Contains(rawStored, []byte(secret)) {
		t.Fatalf("the raw DB-stored value contains the plaintext password — not encrypted at rest: %s", rawStored)
	}
	if bytes.Contains(rawStored, []byte("smtp.example.com")) {
		t.Fatalf("the raw DB-stored value contains plaintext host — not encrypted at rest: %s", rawStored)
	}
}

func TestGetEncrypted_RoundTrips(t *testing.T) {
	db := newFakeDB()
	s := NewSettings(db)
	ctx := context.Background()

	want := smtpFixture{Host: "smtp.example.com", Password: "correct-horse-battery-staple"}
	if err := SetEncrypted(ctx, s, "notifications.smtp", want); err != nil {
		t.Fatalf("SetEncrypted: %v", err)
	}

	got, err := GetEncrypted[smtpFixture](ctx, s, "notifications.smtp")
	if err != nil {
		t.Fatalf("GetEncrypted: %v", err)
	}
	if got != want {
		t.Fatalf("GetEncrypted round-trip mismatch: got %+v, want %+v", got, want)
	}
}

func TestGetEncrypted_MissingKeyReturnsErrSettingNotFound(t *testing.T) {
	s := NewSettings(newFakeDB())
	_, err := GetEncrypted[smtpFixture](context.Background(), s, "notifications.smtp")
	if err == nil {
		t.Fatal("expected an error for a never-set key")
	}
}

// TestGetEncrypted_TamperedCiphertextFailsClosed confirms a corrupted/
// tampered stored value is refused rather than silently decoding into
// garbage credentials.
func TestGetEncrypted_TamperedCiphertextFailsClosed(t *testing.T) {
	db := newFakeDB()
	s := NewSettings(db)
	ctx := context.Background()

	if err := SetEncrypted(ctx, s, "notifications.smtp", smtpFixture{Host: "h", Password: "p"}); err != nil {
		t.Fatalf("SetEncrypted: %v", err)
	}
	db.mu.Lock()
	tampered := append([]byte(nil), db.store["notifications.smtp"]...)
	if len(tampered) > 4 {
		tampered[len(tampered)-2] ^= 0xFF // flip a byte inside the base64 ciphertext string
	}
	db.store["notifications.smtp"] = tampered
	db.mu.Unlock()
	s.Invalidate("notifications.smtp")

	if _, err := GetEncrypted[smtpFixture](ctx, s, "notifications.smtp"); err == nil {
		t.Fatal("GetEncrypted accepted a tampered ciphertext")
	}
}
