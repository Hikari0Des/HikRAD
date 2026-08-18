package cards

// Code hashing (FR-136) — deliberately the IDENTICAL scheme
// billing/voucher.go established for vouchers (sha256 of the normalized,
// upper-cased, separator-stripped code): migration 0605 copies
// voucher_batches/vouchers rows into card_groups/cards by copying
// code_hash VERBATIM, which is only valid if both packages hash the same
// plaintext the same way. Duplicated here (not imported from billing) to
// avoid a cross-package dependency for an 8-line pure function — the two
// packages otherwise share no code, matching the "small pure functions are
// duplicated per package" convention already used for rateString/strOr/intOr
// across profiles, subscribers and billing.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"strings"
)

const (
	numericAlphabet      = "0123456789"
	alphaAlphabet        = "ABCDEFGHJKLMNPQRSTUVWXYZ" // no I/O (ambiguous with 1/0)
	alphanumericAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
)

func alphabetFor(codeFormat string) string {
	switch codeFormat {
	case "numeric":
		return numericAlphabet
	case "alpha":
		return alphaAlphabet
	default: // "alphanumeric" and any unrecognized value
		return alphanumericAlphabet
	}
}

// genCode produces one plaintext card code: an optional (upper-cased) prefix
// followed by random characters from the group's chosen alphabet, padded out
// to codeLength total (never shorter than the prefix + 4, so a very long
// prefix still yields a code with real entropy).
func genCode(prefix, codeFormat string, codeLength int) string {
	prefix = strings.ToUpper(prefix)
	alphabet := alphabetFor(codeFormat)
	n := codeLength - len(prefix)
	if n < 4 {
		n = 4
	}
	var b strings.Builder
	b.WriteString(prefix)
	max := big.NewInt(int64(len(alphabet)))
	for i := 0; i < n; i++ {
		idx, _ := rand.Int(rand.Reader, max)
		b.WriteByte(alphabet[idx.Int64()])
	}
	return b.String()
}

// normalizeCode canonicalizes a card code for hashing — identical to
// billing/voucher.go's normalizeCode: upper-case, then drop everything
// outside [A-Z0-9] so printed/grouped codes ("ABCD-1234") compare equal to
// however the subscriber types them.
func normalizeCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// hashCode is the at-rest representation (sha256 hex of the normalized code).
func hashCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeCode(code)))
	return hex.EncodeToString(sum[:])
}
