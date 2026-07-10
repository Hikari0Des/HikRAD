package auth

// Manager/panel password hashing (NFR-4.1: argon2id, never reversible).
//
// Stored format is a PHC string. New hashes are argon2id:
//
//	$argon2id$v=19$m=65536,t=1,p=4$<b64salt>$<b64hash>
//
// The Phase-1 seed wrote bcrypt hashes, so verifyPassword also accepts bcrypt
// and flags it for transparent upgrade to argon2id on the next successful
// login (the "upgrade path" — see login.go).

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// argon2 parameters for new hashes. Tuned for an interactive panel login on
// the NFR-3 hardware tier; bumping any of these makes older hashes report
// needsUpgrade so they re-hash on next login.
const (
	argonMemoryKiB = 64 * 1024 // 64 MiB
	argonTime      = 1
	argonThreads   = 4
	argonSaltLen   = 16
	argonKeyLen    = 32
)

var errUnknownHashFormat = errors.New("auth: unrecognized password hash format")

// hashPassword returns an argon2id PHC string for pw.
func hashPassword(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: salt: %w", err)
	}
	hash := argon2.IDKey([]byte(pw), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// verifyPassword checks pw against a stored hash (argon2id or legacy bcrypt).
// It returns (matched, needsUpgrade, err). needsUpgrade is true when the hash
// verified but is not in the current argon2id format/parameters, so the caller
// should re-hash and persist.
func verifyPassword(encoded, pw string) (matched, needsUpgrade bool, err error) {
	switch {
	case strings.HasPrefix(encoded, "$argon2id$"):
		return verifyArgon2id(encoded, pw)
	case strings.HasPrefix(encoded, "$2a$"),
		strings.HasPrefix(encoded, "$2b$"),
		strings.HasPrefix(encoded, "$2y$"):
		if berr := bcrypt.CompareHashAndPassword([]byte(encoded), []byte(pw)); berr != nil {
			if errors.Is(berr, bcrypt.ErrMismatchedHashAndPassword) {
				return false, false, nil
			}
			return false, false, berr
		}
		// Correct bcrypt password → migrate to argon2id.
		return true, true, nil
	default:
		return false, false, errUnknownHashFormat
	}
}

func verifyArgon2id(encoded, pw string) (matched, needsUpgrade bool, err error) {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash]
	if len(parts) != 6 {
		return false, false, errUnknownHashFormat
	}
	var version int
	if _, err = fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, false, errUnknownHashFormat
	}
	if version != argon2.Version {
		return false, false, errUnknownHashFormat
	}
	var mem, t uint32
	var p uint8
	if _, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &p); err != nil {
		return false, false, errUnknownHashFormat
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, false, errUnknownHashFormat
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, false, errUnknownHashFormat
	}
	got := argon2.IDKey([]byte(pw), salt, t, mem, p, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false, nil
	}
	// Verified. Flag for re-hash if the stored parameters are weaker than
	// the current policy (a Phase-3 parameter bump auto-upgrades on login).
	upgrade := mem != argonMemoryKiB || t != argonTime || p != argonThreads || len(want) != argonKeyLen
	return true, upgrade, nil
}
