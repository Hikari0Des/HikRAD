package crypto

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

// key256 is the fixed test key 0x00,0x01,…,0x1f.
func key256() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i)
	}
	return k
}

func TestRoundTrip(t *testing.T) {
	s, err := New(key256())
	if err != nil {
		t.Fatal(err)
	}
	for _, pt := range [][]byte{
		[]byte(""),
		[]byte("p"),
		[]byte("correct horse battery staple"),
		[]byte("پارسی و العربية — non-ASCII secret"),
		bytes.Repeat([]byte{0}, 1024),
	} {
		ct, err := s.Encrypt(pt)
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		if ct[0] != versionAESGCM {
			t.Fatalf("missing version prefix: 0x%02x", ct[0])
		}
		got, err := s.Decrypt(ct)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}
		if !bytes.Equal(got, pt) {
			t.Fatalf("round trip: got %q want %q", got, pt)
		}
	}
}

// TestFixedVector is a known-answer test: a ciphertext produced once (fixed
// key + fixed nonce, AES-256-GCM, documented version||nonce||ct layout) must
// keep decrypting to the same plaintext. Guards against an accidental layout
// change breaking already-stored secrets.
func TestFixedVector(t *testing.T) {
	const vectorHex = "01a0a1a2a3a4a5a6a7a8a9aaab8e71175f24af2fcc0706f5b67357f4ec9913dc5f7f8f4408284b429fbb7cc409"
	const wantPlain = "hikrad-secret-42"
	ct, err := hex.DecodeString(vectorHex)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(key256())
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Decrypt(ct)
	if err != nil {
		t.Fatalf("decrypt fixed vector: %v", err)
	}
	if string(got) != wantPlain {
		t.Fatalf("fixed vector: got %q want %q", got, wantPlain)
	}
}

func TestTamperDetected(t *testing.T) {
	s, _ := New(key256())
	ct, _ := s.Encrypt([]byte("tamper me"))
	// Flip a bit in the last (tag) byte.
	tampered := bytes.Clone(ct)
	tampered[len(tampered)-1] ^= 0x01
	if _, err := s.Decrypt(tampered); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("tampered ciphertext must fail with ErrDecrypt, got %v", err)
	}
	// Flip a bit in the ciphertext body.
	tampered2 := bytes.Clone(ct)
	tampered2[len(tampered2)/2] ^= 0x01
	if _, err := s.Decrypt(tampered2); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("tampered body must fail, got %v", err)
	}
}

func TestWrongKeyFails(t *testing.T) {
	s1, _ := New(key256())
	other := key256()
	other[0] ^= 0xFF
	s2, _ := New(other)
	ct, _ := s1.Encrypt([]byte("secret"))
	if _, err := s2.Decrypt(ct); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("wrong key must fail with ErrDecrypt, got %v", err)
	}
}

func TestDecryptRejectsMalformed(t *testing.T) {
	s, _ := New(key256())
	cases := map[string][]byte{
		"empty":           {},
		"unknown version": append([]byte{0x02}, bytes.Repeat([]byte{0}, 40)...),
		"truncated nonce": {versionAESGCM, 0x00, 0x00},
	}
	for name, in := range cases {
		if _, err := s.Decrypt(in); !errors.Is(err, ErrDecrypt) {
			t.Fatalf("%s: want ErrDecrypt, got %v", name, err)
		}
	}
}

func TestNewRejectsBadKeyLength(t *testing.T) {
	for _, n := range []int{0, 16, 24, 31, 33} {
		if _, err := New(make([]byte, n)); err == nil {
			t.Fatalf("New must reject %d-byte key", n)
		}
	}
}

func TestNonceUniqueness(t *testing.T) {
	s, _ := New(key256())
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		ct, _ := s.Encrypt([]byte("x"))
		nonce := string(ct[1:13])
		if seen[nonce] {
			t.Fatal("nonce reused across encryptions")
		}
		seen[nonce] = true
	}
}

func TestPackageLevelConfigure(t *testing.T) {
	if err := Configure(key256()); err != nil {
		t.Fatal(err)
	}
	ct, err := Encrypt([]byte("via package default"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(ct)
	if err != nil || string(got) != "via package default" {
		t.Fatalf("package round trip: %q %v", got, err)
	}
}
