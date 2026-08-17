package platform

// Encrypted settings values (FR-127, phase v4-3): a small extension of the
// Get[T]/Settings.Set pair that seals the JSON value under crypto's AES-GCM
// envelope before it ever reaches the settings table, and opens it again on
// read — same "a DB copy without HIKRAD_ENCRYPTION_KEY yields nothing"
// guarantee NFR-4.2/4.3 already gives subscriber passwords/NAS secrets/TOTP
// secrets, extended to the notification-channel credentials embedded inside
// the notifications.{smtp,telegram,whatsapp} settings objects (found not to
// be encrypted while auditing the FR-127 inventory — every other secret
// this package stores went through crypto.Encrypt already, these three
// field's credential sub-fields did not).
//
// Deliberately encrypts the WHOLE stored JSON value, not just a credential
// sub-field: this keeps the external GET/PUT wire contract for these
// settings groups byte-identical to before (the panel's existing
// NotificationsSettings.tsx round-trips the full smtp/telegram/whatsapp
// object unchanged) while what actually lands on disk in Postgres is
// opaque ciphertext — a narrower, lower-risk fix than reshaping the API to
// split out a secret sub-field, which is achievable in a bounded diff
// without touching frontend behavior at all.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/hikrad/hikrad/internal/platform/crypto"
)

// SetEncrypted marshals v, seals it with crypto.Encrypt, and stores the
// resulting ciphertext (base64-encoded, as a JSON string) under key.
func SetEncrypted(ctx context.Context, s Settings, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("platform: encode encrypted setting %q: %w", key, err)
	}
	return SetEncryptedRaw(ctx, s, key, raw)
}

// SetEncryptedRaw seals the exact bytes of raw (already-marshaled JSON) and
// stores the ciphertext under key — used where the caller has a
// json.RawMessage it wants to store opaquely without an intermediate Go
// struct (e.g. the generic settings-group PUT handler, which accepts
// arbitrary nested shapes for the notifications group).
func SetEncryptedRaw(ctx context.Context, s Settings, key string, raw json.RawMessage) error {
	enc, err := crypto.Encrypt(raw)
	if err != nil {
		return fmt.Errorf("platform: encrypt setting %q: %w", key, err)
	}
	return s.Set(ctx, key, base64.StdEncoding.EncodeToString(enc))
}

// GetEncrypted is Get[T]'s encrypted counterpart: reads the ciphertext
// stored by SetEncrypted/SetEncryptedRaw, opens it, and decodes the result
// into T. Returns ErrSettingNotFound unchanged when key has never been set,
// same as Get[T], so existing "not configured yet" call sites need no
// changes beyond swapping which function they call.
func GetEncrypted[T any](ctx context.Context, s Settings, key string) (T, error) {
	var v T
	raw, err := GetEncryptedRaw(ctx, s, key)
	if err != nil {
		return v, err
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, fmt.Errorf("platform: setting %q: decode into %T: %w", key, v, err)
	}
	return v, nil
}

// GetEncryptedRaw returns the decrypted JSON bytes for key, without
// decoding into a struct — the settings-group GET handler's counterpart to
// SetEncryptedRaw, so it can hand the panel back the exact decrypted JSON
// it originally sent, unchanged.
func GetEncryptedRaw(ctx context.Context, s Settings, key string) (json.RawMessage, error) {
	raw, err := s.GetRaw(ctx, key)
	if err != nil {
		return nil, err
	}
	var b64 string
	if err := json.Unmarshal(raw, &b64); err != nil {
		return nil, fmt.Errorf("platform: encrypted setting %q is not a string: %w", key, err)
	}
	enc, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("platform: encrypted setting %q: bad base64: %w", key, err)
	}
	dec, err := crypto.Decrypt(enc)
	if err != nil {
		return nil, fmt.Errorf("platform: decrypt setting %q: %w", key, err)
	}
	return json.RawMessage(dec), nil
}
