package seed

// TEMPORARY LOCATION (documented per the Phase-1 Agent-3 task): this
// AES-GCM helper lives in the seed package only for Phase 1 and moves to
// Agent 1's platform crypto service in Phase 2. Subscriber RADIUS passwords
// are reversibly encrypted (NFR-4.2) because CHAP needs the cleartext at
// authorize time; decryption is allowed only in the authorize path.
//
// The key is platform.Config.EncryptionKey: HIKRAD_ENCRYPTION_KEY decoded
// from base64 to exactly 32 bytes (AES-256), validated by LoadConfig.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// EncryptPassword seals plaintext with AES-256-GCM.
// Output layout: nonce || ciphertext.
func EncryptPassword(plaintext string, key []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// DecryptPassword reverses EncryptPassword.
func DecryptPassword(data []byte, key []byte) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes (AES-256), got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
