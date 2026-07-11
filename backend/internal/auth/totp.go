package auth

// TOTP 2FA core (FR-28.1, RFC 6238) — HMAC-SHA1, 6 digits, 30-second step,
// ±1 step clock-skew tolerance. Implemented on the standard library (no new
// dependency). Secrets are 20 random bytes, base32-encoded (the format
// authenticator apps expect) and AES-GCM sealed at rest (NFR-4.3).

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // HMAC-SHA1 is the RFC 6238 / authenticator-app standard
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	totpDigits    = 6
	totpPeriod    = 30 * time.Second
	totpSkewSteps = 1 // accept the previous/next window (clock skew)
	totpSecretLen = 20
	totpIssuer    = "HikRAD"
)

// base32NoPad is the encoding used for the shared secret (uppercase, no '=').
var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

// generateTOTPSecret returns a fresh base32-encoded shared secret.
func generateTOTPSecret() (string, error) {
	buf := make([]byte, totpSecretLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base32NoPad.EncodeToString(buf), nil
}

// hotp computes the HOTP value for a counter (RFC 4226).
func hotp(key []byte, counter uint64) int {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]) << 16) |
		(uint32(sum[offset+2]) << 8) |
		uint32(sum[offset+3])
	return int(code % 1_000_000) // totpDigits == 6
}

// totpCodeAt renders the TOTP code for a given time.
func totpCodeAt(secret string, t time.Time) (string, error) {
	key, err := base32NoPad.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", fmt.Errorf("auth: bad totp secret: %w", err)
	}
	counter := uint64(t.Unix()) / uint64(totpPeriod.Seconds())
	return fmt.Sprintf("%0*d", totpDigits, hotp(key, counter)), nil
}

// verifyTOTP checks code against secret at time now, tolerating ±totpSkewSteps
// windows. Comparison is constant-time.
func verifyTOTP(secret, code string, now time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return false
	}
	key, err := base32NoPad.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return false
	}
	step := uint64(totpPeriod.Seconds())
	base := uint64(now.Unix()) / step
	for d := -totpSkewSteps; d <= totpSkewSteps; d++ {
		counter := uint64(int64(base) + int64(d))
		want := fmt.Sprintf("%0*d", totpDigits, hotp(key, counter))
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// otpauthURI builds the provisioning URI encoded in the enrolment QR code.
func otpauthURI(account, secret string) string {
	label := url.PathEscape(totpIssuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", totpIssuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprintf("%d", totpDigits))
	q.Set("period", fmt.Sprintf("%d", int(totpPeriod.Seconds())))
	return "otpauth://totp/" + label + "?" + q.Encode()
}
