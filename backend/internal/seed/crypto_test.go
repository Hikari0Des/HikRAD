package seed

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := testKey(t)
	for _, plaintext := range []string{"testpass", "", "كلمة السر", "p@ss w0rd\n"} {
		enc, err := EncryptPassword(plaintext, key)
		if err != nil {
			t.Fatalf("encrypt %q: %v", plaintext, err)
		}
		got, err := DecryptPassword(enc, key)
		if err != nil {
			t.Fatalf("decrypt %q: %v", plaintext, err)
		}
		if got != plaintext {
			t.Fatalf("round trip = %q, want %q", got, plaintext)
		}
	}
}

func TestEncryptIsNonDeterministic(t *testing.T) {
	key := testKey(t)
	a, _ := EncryptPassword("testpass", key)
	b, _ := EncryptPassword("testpass", key)
	if bytes.Equal(a, b) {
		t.Fatal("two encryptions of the same plaintext must differ (random nonce)")
	}
}

func TestDecryptRejectsTamperAndWrongKey(t *testing.T) {
	key := testKey(t)
	enc, err := EncryptPassword("testpass", key)
	if err != nil {
		t.Fatal(err)
	}

	tampered := append([]byte{}, enc...)
	tampered[len(tampered)-1] ^= 0x01
	if _, err := DecryptPassword(tampered, key); err == nil {
		t.Fatal("tampered ciphertext must not decrypt")
	}

	if _, err := DecryptPassword(enc, testKey(t)); err == nil {
		t.Fatal("wrong key must not decrypt")
	}

	if _, err := DecryptPassword([]byte{1, 2, 3}, key); err == nil {
		t.Fatal("short ciphertext must error")
	}
}

func TestKeyMustBe32Bytes(t *testing.T) {
	if _, err := EncryptPassword("x", []byte("short")); err == nil {
		t.Fatal("non-32-byte key must be rejected")
	}
}
