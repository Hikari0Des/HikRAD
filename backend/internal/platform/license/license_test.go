package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func testKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	return pub, priv
}

func samplePayload() Payload {
	return Payload{
		KeyID:           "K-1",
		Licensee:        "Test ISP",
		IssuedAt:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Tier:            "5k",
		MaxSubscribers:  5000,
		EntitledVersion: "1",
		Fingerprint:     Compose("machine-a", "aa:bb:cc:dd:ee:ff"),
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv := testKeypair(t)
	blob, err := Sign(priv, samplePayload())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	got, err := Verify(pub, blob)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Licensee != "Test ISP" || got.KeyID != "K-1" {
		t.Errorf("Verify returned %+v", got)
	}
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	pub, priv := testKeypair(t)
	blob, err := Sign(priv, samplePayload())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	var p Payload
	if err := json.Unmarshal(blob.Payload, &p); err != nil {
		t.Fatal(err)
	}
	p.MaxSubscribers = 999999 // attacker bumps the tier post-signature
	tampered, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	blob.Payload = tampered

	if _, err := Verify(pub, blob); err == nil {
		t.Fatal("Verify accepted a tampered payload")
	} else if err != ErrInvalidSignature {
		t.Errorf("err = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	_, priv := testKeypair(t)
	otherPub, _ := testKeypair(t)
	blob, err := Sign(priv, samplePayload())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if _, err := Verify(otherPub, blob); err != ErrInvalidSignature {
		t.Errorf("err = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyRejectsBadSignatureEncoding(t *testing.T) {
	_, priv := testKeypair(t)
	pub := priv.Public().(ed25519.PublicKey)
	blob, err := Sign(priv, samplePayload())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	blob.Signature = "not-base64!!!"
	if _, err := Verify(pub, blob); err == nil {
		t.Fatal("Verify accepted an undecodable signature")
	}
}

func TestVerifyRejectsMalformedPayload(t *testing.T) {
	pub, priv := testKeypair(t)
	sig := ed25519.Sign(priv, []byte("not json"))
	blob := Blob{Payload: []byte("not json"), Signature: base64.StdEncoding.EncodeToString(sig)}
	if _, err := Verify(pub, blob); err == nil {
		t.Fatal("Verify accepted malformed JSON payload")
	}
}

func TestVerifyRejectsMissingRequiredFields(t *testing.T) {
	pub, priv := testKeypair(t)
	p := samplePayload()
	p.Fingerprint = ""
	blob, err := Sign(priv, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(pub, blob); err == nil {
		t.Fatal("Verify accepted a payload with an empty fingerprint")
	}
}

func TestProductionPublicKeyDecodes(t *testing.T) {
	if len(ProductionPublicKey) != ed25519.PublicKeySize {
		t.Fatalf("ProductionPublicKey is %d bytes, want %d", len(ProductionPublicKey), ed25519.PublicKeySize)
	}
}

// --- Fingerprint tolerance matrix -------------------------------------------

func TestWithinToleranceMatrix(t *testing.T) {
	base := Compose("machine-a", "aa:bb:cc:dd:ee:ff")
	sameMachineNewMAC := Compose("machine-a", "11:22:33:44:55:66") // VM clone: hypervisor reassigns MAC
	newMachineSameMAC := Compose("machine-b", "aa:bb:cc:dd:ee:ff") // disk migrated to new hardware, MAC follows a passthrough NIC
	bothChanged := Compose("machine-b", "11:22:33:44:55:66")       // genuinely different server

	cases := []struct {
		name string
		got  string
		want bool
	}{
		{"exact match", base, true},
		{"single component: MAC changed", sameMachineNewMAC, true},
		{"single component: machine-id changed", newMachineSameMAC, true},
		{"both components changed", bothChanged, false},
		{"malformed stored value", "not-a-fingerprint", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := WithinTolerance(base, c.got); got != c.want {
				t.Errorf("WithinTolerance(base, %q) = %v, want %v", c.got, got, c.want)
			}
		})
	}
}

func TestComposeDeterministic(t *testing.T) {
	a := Compose("m1", "mac1")
	b := Compose("m1", "mac1")
	if a != b {
		t.Errorf("Compose is not deterministic: %q != %q", a, b)
	}
	c := Compose("m2", "mac1")
	if a == c {
		t.Error("Compose did not change when machine-id changed")
	}
}

// TestCompareComponents covers the FR-104 per-component drift view used by the
// License page to name what changed (machine-id vs MAC vs both).
func TestCompareComponents(t *testing.T) {
	base := Compose("machine-a", "mac-a")
	midDrift := Compose("machine-b", "mac-a")
	macDrift := Compose("machine-a", "mac-b")
	both := Compose("machine-b", "mac-b")

	if mid, mac := CompareComponents(base, base); !mid || !mac {
		t.Fatalf("identical should match both, got mid=%v mac=%v", mid, mac)
	}
	if mid, mac := CompareComponents(base, midDrift); mid || !mac {
		t.Fatalf("machine-id drift: want mid=false mac=true, got mid=%v mac=%v", mid, mac)
	}
	if mid, mac := CompareComponents(base, macDrift); !mid || mac {
		t.Fatalf("MAC drift: want mid=true mac=false, got mid=%v mac=%v", mid, mac)
	}
	if mid, mac := CompareComponents(base, both); mid || mac {
		t.Fatalf("both drift: want both false, got mid=%v mac=%v", mid, mac)
	}
	// Malformed input yields both-false rather than panicking.
	if mid, mac := CompareComponents("nosep", base); mid || mac {
		t.Fatalf("malformed should be both-false, got mid=%v mac=%v", mid, mac)
	}
}
