package radius

// stub_policy.go is the Phase-1 authorize decision, clearly scoped to be
// replaced in Phase 2: the only subscriber that can ever accept is whichever
// row the harness/seed created, looked up live from the C6 `subscribers`
// table (no profile, expiry, status, or session-limit checks yet — that is
// the full policy engine). Keeping the DB round-trip real here (rather than
// hardcoding the string "testuser") is what lets the packet harness exercise
// the actual decrypt path end to end.

import (
	"context"
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"errors"

	"github.com/hikrad/hikrad/internal/seed"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type subscriberRecord struct {
	Username    string
	PasswordEnc []byte
}

// lookupSubscriber is a package variable so unit tests can stub the database
// (mirrors httpapi.lookupManager).
var lookupSubscriber = func(ctx context.Context, db *pgxpool.Pool, username string) (*subscriberRecord, error) {
	var s subscriberRecord
	err := db.QueryRow(ctx,
		`SELECT username::text, password_enc FROM subscribers WHERE username = $1`,
		username,
	).Scan(&s.Username, &s.PasswordEnc)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// decide runs the Phase-1 stub policy. A non-nil error means infrastructure
// failure (DB unreachable, corrupt ciphertext) — the caller turns that into a
// 500 so FreeRADIUS's rlm_rest timeout (2s) rejects the packet instead of
// hanging (agent-2 task file edge case); a legitimate accept/reject decision
// is always returned as a value, never an error.
func decide(ctx context.Context, db *pgxpool.Pool, encKey []byte, req authorizeRequest) (authorizeResponse, error) {
	sub, err := lookupSubscriber(ctx, db, req.Username)
	if err != nil {
		return authorizeResponse{}, err
	}
	if sub == nil {
		return reject(ReasonUnknownUser), nil
	}

	plaintext, err := seed.DecryptPassword(sub.PasswordEnc, encKey)
	if err != nil {
		return authorizeResponse{}, err
	}

	var ok bool
	if req.ChapResponse != "" {
		ok, err = verifyCHAP(plaintext, req.ChapChallenge, req.ChapResponse)
		if err != nil {
			// Malformed CHAP fields are a client error, not infra failure.
			return reject(ReasonBadPassword), nil
		}
	} else {
		ok = subtle.ConstantTimeCompare([]byte(plaintext), []byte(req.Password)) == 1
	}
	if !ok {
		return reject(ReasonBadPassword), nil
	}

	return authorizeResponse{
		Action: "accept",
		Reason: ReasonOK,
		Attributes: []attribute{
			{Intent: string(IntentRateLimit), Value: "10M/10M"},
		},
	}, nil
}

func reject(reason string) authorizeResponse {
	return authorizeResponse{Action: "reject", Reason: reason, Attributes: []attribute{}}
}

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
