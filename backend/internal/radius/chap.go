package radius

import (
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"errors"
)

// verifyCHAP checks a CHAP-Password/CHAP-Challenge pair against the known
// cleartext password. This is the concrete proof of the NFR-4.2 decision:
// CHAP needs the plaintext at authorize time, which is exactly why subscriber
// passwords are reversibly encrypted rather than hashed.
//
// chapResponseHex decodes to the 17-byte CHAP-Password value (1-byte ident +
// 16-byte MD5 digest); chapChallengeHex decodes to the CHAP-Challenge bytes.
// digest = MD5(ident || password || challenge), per RFC 2865 §2.2.
func verifyCHAP(password, chapChallengeHex, chapResponseHex string) (bool, error) {
	resp, err := hex.DecodeString(chapResponseHex)
	if err != nil || len(resp) != 17 {
		return false, errors.New("malformed chap_response")
	}
	challenge, err := hex.DecodeString(chapChallengeHex)
	if err != nil {
		return false, errors.New("malformed chap_challenge")
	}
	h := md5.New()
	h.Write(resp[:1])
	h.Write([]byte(password))
	h.Write(challenge)
	expected := h.Sum(nil)
	return subtle.ConstantTimeCompare(expected, resp[1:]) == 1, nil
}
