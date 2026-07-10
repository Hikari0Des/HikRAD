package auth

import (
	"testing"
	"time"
)

func TestAccessTokenRoundTrip(t *testing.T) {
	ts := newTokenService([]byte("secret"))
	tok, err := ts.issueAccess(accessClaims{
		ManagerID: "m1", Role: RoleAgent, Scoped: true, SessionID: "s1",
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	c, err := ts.parseAccess(tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.ManagerID != "m1" || c.Role != RoleAgent || !c.Scoped || c.SessionID != "s1" {
		t.Fatalf("claims = %+v", c)
	}
}

func TestAccessTokenRejectsWrongSecretAndExpiry(t *testing.T) {
	ts := newTokenService([]byte("secret"))
	other := newTokenService([]byte("other"))

	tok, _ := ts.issueAccess(accessClaims{ManagerID: "m1", Role: RoleAdmin, SessionID: "s"}, time.Now())
	if _, err := other.parseAccess(tok); err == nil {
		t.Fatal("token verified under wrong secret")
	}

	expired, _ := ts.issueAccess(accessClaims{ManagerID: "m1", Role: RoleAdmin, SessionID: "s"}, time.Now().Add(-2*accessTTL))
	if _, err := ts.parseAccess(expired); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestRefreshTokenCompose(t *testing.T) {
	secret, hash, err := newRefreshSecret()
	if err != nil {
		t.Fatal(err)
	}
	tok := composeRefreshToken("sess-1", secret)
	sid, sec, ok := parseRefreshToken(tok)
	if !ok || sid != "sess-1" || sec != secret {
		t.Fatalf("parse = %q %q %v", sid, sec, ok)
	}
	// Stored hash must match a recompute of the same secret and differ for another.
	if string(hashRefresh(secret)) != string(hash) {
		t.Fatal("hashRefresh not stable")
	}
	if string(hashRefresh("different")) == string(hash) {
		t.Fatal("hash collision")
	}
}

func TestParseRefreshTokenRejectsMalformed(t *testing.T) {
	for _, in := range []string{"", "nodot", ".onlysecret", "onlyid.", "."} {
		if _, _, ok := parseRefreshToken(in); ok {
			t.Fatalf("malformed token accepted: %q", in)
		}
	}
}
