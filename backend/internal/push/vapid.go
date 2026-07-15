package push

// VAPID identity (RFC 8292): one persistent ECDSA P-256 keypair per
// installation, generated on first boot and sealed into settings (contract
// C4). The private key signs a short-lived ES256 JWT per push send that
// proves server identity to the push service; the public key travels as the
// `k=` Authorization param and as the browser's `applicationServerKey`.
//
// Edge case (VAPID key rotation): regenerating invalidates every existing
// subscription (the browser binds applicationServerKey at subscribe time), so
// EnsureKeys never overwrites a key that already decodes successfully —
// rotation is an explicit settings action outside this package's scope
// (documented in the admin guide per the task brief).

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/platform/crypto"
)

const vapidSettingsKey = "push.vapid"

// vapidSubject is the JWT "sub" claim (RFC 8292 requires a contact URI). Not
// settings-driven — this is a protocol-required contact, not ISP branding.
const vapidSubject = "mailto:support@hikrad.local"

type vapidStored struct {
	// SealedD is crypto.Encrypt(D.Bytes()) — the private scalar, base64
	// std-encoded envelope (it's a private key, so it is sealed like every
	// other secret at rest, NFR-4.2, unlike the subscription p256dh/auth which
	// are not server secrets).
	SealedD  string `json:"sealed_d"`
	PublicKB string `json:"public_k"` // base64url uncompressed point (65 bytes), the `k=` value
}

// keypair caches the decoded VAPID identity for the process lifetime; settings
// reads are cheap (cached) but ECDSA reconstruction is pure CPU work worth
// avoiding per send.
var (
	kpMu sync.Mutex
	kp   *ecdsa.PrivateKey
	kpB  string // cached public_k
)

// EnsureKeys returns the process's VAPID keypair, generating and persisting
// one on first call if settings has none yet (idempotent bootstrap: a second
// call, in this process or a fresh one, reuses the stored key rather than
// regenerating — verified by TestEnsureKeys_Idempotent).
func EnsureKeys(ctx context.Context, settings platform.Settings) (*ecdsa.PrivateKey, string, error) {
	kpMu.Lock()
	defer kpMu.Unlock()
	if kp != nil {
		return kp, kpB, nil
	}

	raw, err := settings.GetRaw(ctx, vapidSettingsKey)
	if err == nil {
		var stored vapidStored
		if json.Unmarshal(raw, &stored) == nil && stored.SealedD != "" {
			priv, pubB64, derr := decodeStored(stored)
			if derr == nil {
				kp, kpB = priv, pubB64
				return kp, kpB, nil
			}
			// Fall through to regenerate only if decode genuinely fails (e.g. key
			// rotated at the OS/env level) — logged by the caller, not here.
		}
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("push: generate VAPID key: %w", err)
	}
	pub, err := priv.PublicKey.ECDH()
	if err != nil {
		return nil, "", fmt.Errorf("push: derive VAPID public key: %w", err)
	}
	pubBytes := pub.Bytes()
	sealed, err := crypto.Encrypt(priv.D.Bytes())
	if err != nil {
		return nil, "", fmt.Errorf("push: seal VAPID private key: %w", err)
	}
	stored := vapidStored{
		SealedD:  base64.StdEncoding.EncodeToString(sealed),
		PublicKB: base64.RawURLEncoding.EncodeToString(pubBytes),
	}
	if err := settings.Set(ctx, vapidSettingsKey, stored); err != nil {
		return nil, "", fmt.Errorf("push: persist VAPID key: %w", err)
	}
	kp, kpB = priv, stored.PublicKB
	return kp, kpB, nil
}

func decodeStored(stored vapidStored) (*ecdsa.PrivateKey, string, error) {
	sealed, err := base64.StdEncoding.DecodeString(stored.SealedD)
	if err != nil {
		return nil, "", err
	}
	dBytes, err := crypto.Decrypt(sealed)
	if err != nil {
		return nil, "", err
	}
	d := new(big.Int).SetBytes(dBytes)
	priv := new(ecdsa.PrivateKey)
	priv.PublicKey.Curve = elliptic.P256()
	priv.D = d
	priv.PublicKey.X, priv.PublicKey.Y = elliptic.P256().ScalarBaseMult(dBytes)
	return priv, stored.PublicKB, nil
}

// resetCache is test-only: forces the next EnsureKeys to re-read settings.
func resetCache() {
	kpMu.Lock()
	kp, kpB = nil, ""
	kpMu.Unlock()
}

// vapidAuthHeader builds the RFC 8292 `Authorization: vapid t=..., k=...`
// value for one push endpoint. aud must be the scheme://host of the endpoint.
func vapidAuthHeader(priv *ecdsa.PrivateKey, pubB64, aud string) (string, error) {
	claims := jwt.MapClaims{
		"aud": aud,
		"exp": time.Now().Add(12 * time.Hour).Unix(),
		"sub": vapidSubject,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	signed, err := tok.SignedString(priv)
	if err != nil {
		return "", fmt.Errorf("push: sign VAPID JWT: %w", err)
	}
	return "vapid t=" + signed + ", k=" + pubB64, nil
}
