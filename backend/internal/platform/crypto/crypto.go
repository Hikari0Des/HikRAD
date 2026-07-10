// Package crypto is the platform AES-GCM envelope service (Phase 2, Agent 1;
// contract C3, NFR-4.2/4.3). It seals secrets at rest — subscriber RADIUS
// passwords (reversible for CHAP, decrypted only in the authorize path),
// NAS shared secrets, SNMP communities, TOTP secrets, and gateway
// credentials — under HIKRAD_ENCRYPTION_KEY.
//
// It replaces the Phase-1 temporary helper in internal/seed (Agent D
// re-points subscriber password sealing here). Output is versioned so the
// key/algorithm can rotate without a data migration:
//
//	byte 0   : version (0x01 = AES-256-GCM, 12-byte nonce)
//	byte 1.. : nonce || ciphertext+tag  (as produced by cipher.AEAD.Seal)
//
// A DB copy without the key yields nothing (AC-NFR4a): the key lives only in
// server config (.env), never in the database.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"sync"
)

// versionAESGCM is the envelope version prefix for AES-256-GCM with a random
// 12-byte nonce. Future schemes take the next byte value; Decrypt dispatches
// on it so old ciphertexts keep decrypting after a rotation.
const versionAESGCM byte = 0x01

// ErrKeyNotConfigured is returned by the package-level Encrypt/Decrypt when no
// key has been configured (neither via Configure nor HIKRAD_ENCRYPTION_KEY).
var ErrKeyNotConfigured = errors.New("crypto: encryption key not configured")

// ErrDecrypt is returned when a ciphertext cannot be authenticated or decoded
// (wrong key, tampering, truncation, unknown version). It is deliberately
// coarse so callers cannot use the error to distinguish tamper from wrong-key.
var ErrDecrypt = errors.New("crypto: decryption failed")

// Service seals and opens secrets with a fixed 32-byte AES-256 key.
type Service struct {
	gcm cipher.AEAD
}

// New builds a Service from a 32-byte (AES-256) key.
func New(key []byte) (*Service, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: key must be 32 bytes (AES-256), got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Service{gcm: gcm}, nil
}

// Encrypt seals plaintext, returning version || nonce || ciphertext.
func (s *Service) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("crypto: generate nonce: %w", err)
	}
	// Prefix the version byte, then let Seal append the ciphertext after the
	// nonce so the layout is version || nonce || ciphertext in one buffer.
	out := make([]byte, 1, 1+len(nonce)+len(plaintext)+s.gcm.Overhead())
	out[0] = versionAESGCM
	out = append(out, nonce...)
	return s.gcm.Seal(out, nonce, plaintext, nil), nil
}

// Decrypt reverses Encrypt. Any failure (unknown version, truncation, wrong
// key, tampering) returns ErrDecrypt.
func (s *Service) Decrypt(data []byte) ([]byte, error) {
	if len(data) < 1 {
		return nil, ErrDecrypt
	}
	if data[0] != versionAESGCM {
		return nil, fmt.Errorf("%w: unknown version 0x%02x", ErrDecrypt, data[0])
	}
	body := data[1:]
	ns := s.gcm.NonceSize()
	if len(body) < ns {
		return nil, ErrDecrypt
	}
	nonce, ciphertext := body[:ns], body[ns:]
	plaintext, err := s.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}

// --- Package-level default service (frozen C3 API: crypto.Encrypt/Decrypt) ---
//
// Consumers (B: NAS secrets/SNMP; D: subscriber passwords) call the
// package-level functions with no wiring. The default service initializes
// lazily from HIKRAD_ENCRYPTION_KEY on first use, or explicitly via Configure
// (preferred in main/tests so a bad key fails fast at boot).

var (
	defaultMu  sync.RWMutex
	defaultSvc *Service
	initOnce   sync.Once
)

// Configure installs the process-wide default service from a 32-byte key.
// Call once at boot; overrides any lazily-initialized service.
func Configure(key []byte) error {
	svc, err := New(key)
	if err != nil {
		return err
	}
	defaultMu.Lock()
	defaultSvc = svc
	defaultMu.Unlock()
	return nil
}

func def() (*Service, error) {
	defaultMu.RLock()
	svc := defaultSvc
	defaultMu.RUnlock()
	if svc != nil {
		return svc, nil
	}
	initOnce.Do(func() {
		raw := os.Getenv("HIKRAD_ENCRYPTION_KEY")
		if raw == "" {
			return
		}
		key, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return
		}
		if svc, err := New(key); err == nil {
			defaultMu.Lock()
			if defaultSvc == nil {
				defaultSvc = svc
			}
			defaultMu.Unlock()
		}
	})
	defaultMu.RLock()
	svc = defaultSvc
	defaultMu.RUnlock()
	if svc == nil {
		return nil, ErrKeyNotConfigured
	}
	return svc, nil
}

// Encrypt seals plaintext with the process default service (frozen C3 API).
func Encrypt(plaintext []byte) ([]byte, error) {
	svc, err := def()
	if err != nil {
		return nil, err
	}
	return svc.Encrypt(plaintext)
}

// Decrypt opens a ciphertext with the process default service (frozen C3 API).
func Decrypt(ciphertext []byte) ([]byte, error) {
	svc, err := def()
	if err != nil {
		return nil, err
	}
	return svc.Decrypt(ciphertext)
}
