package radius

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/seed"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testKey = []byte("01234567890123456789012345678901")[:32]

func testDeps() httpapi.Deps {
	return httpapi.Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// stubSubscribers replaces the DB lookup with a fixed subscriber set,
// mirroring httpapi's stubManagers pattern.
func stubSubscribers(t *testing.T, byUsername map[string]subscriberRecord) {
	t.Helper()
	orig := lookupSubscriber
	lookupSubscriber = func(_ context.Context, _ *pgxpool.Pool, username string) (*subscriberRecord, error) {
		if s, ok := byUsername[username]; ok {
			return &s, nil
		}
		return nil, nil
	}
	t.Cleanup(func() { lookupSubscriber = orig })
}

func seededSubscriber(t *testing.T, username, password string) map[string]subscriberRecord {
	t.Helper()
	enc, err := seed.EncryptPassword(password, testKey)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]subscriberRecord{username: {Username: username, PasswordEnc: enc}}
}

func postAuthorize(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	h := authorizeHandler(testDeps(), testKey)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/internal/radius/authorize", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, r)
	return rec
}

func decodeResponse(t *testing.T, rec *httptest.ResponseRecorder) authorizeResponse {
	t.Helper()
	var resp authorizeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not valid JSON: %s", rec.Body.String())
	}
	return resp
}

func TestAuthorizePAPAccept(t *testing.T) {
	stubSubscribers(t, seededSubscriber(t, "testuser", "testpass"))

	rec := postAuthorize(t, `{"username":"testuser","password":"testpass","nas_ip":"10.0.0.1","service":"pppoe"}`)
	resp := decodeResponse(t, rec)

	if resp.Action != "accept" || resp.Reason != ReasonOK {
		t.Fatalf("resp = %+v", resp)
	}
	if len(resp.Attributes) != 1 || resp.Attributes[0].Intent != string(IntentRateLimit) || resp.Attributes[0].Value != "10M/10M" {
		t.Fatalf("attributes = %+v", resp.Attributes)
	}
}

func TestAuthorizePAPBadPassword(t *testing.T) {
	stubSubscribers(t, seededSubscriber(t, "testuser", "testpass"))

	rec := postAuthorize(t, `{"username":"testuser","password":"wrong","nas_ip":"10.0.0.1","service":"pppoe"}`)
	resp := decodeResponse(t, rec)

	if resp.Action != "reject" || resp.Reason != ReasonBadPassword {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestAuthorizeUnknownUser(t *testing.T) {
	stubSubscribers(t, map[string]subscriberRecord{})

	rec := postAuthorize(t, `{"username":"ghost","password":"x","nas_ip":"10.0.0.1","service":"pppoe"}`)
	resp := decodeResponse(t, rec)

	if resp.Action != "reject" || resp.Reason != ReasonUnknownUser {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestAuthorizeCHAPAccept(t *testing.T) {
	stubSubscribers(t, seededSubscriber(t, "testuser", "testpass"))

	challenge := []byte("0123456789abcdef")
	const id = 7
	h := md5.New()
	h.Write([]byte{id})
	h.Write([]byte("testpass"))
	h.Write(challenge)
	digest := h.Sum(nil)
	chapResponse := append([]byte{id}, digest...)

	body := `{"username":"testuser","chap_challenge":"` + hex.EncodeToString(challenge) +
		`","chap_response":"` + hex.EncodeToString(chapResponse) + `","nas_ip":"10.0.0.1","service":"pppoe"}`
	rec := postAuthorize(t, body)
	resp := decodeResponse(t, rec)

	if resp.Action != "accept" || resp.Reason != ReasonOK {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestAuthorizeCHAPBadResponse(t *testing.T) {
	stubSubscribers(t, seededSubscriber(t, "testuser", "testpass"))

	body := `{"username":"testuser","chap_challenge":"0011223344556677","chap_response":"00` +
		strings.Repeat("ff", 16) + `","nas_ip":"10.0.0.1","service":"pppoe"}`
	rec := postAuthorize(t, body)
	resp := decodeResponse(t, rec)

	if resp.Action != "reject" || resp.Reason != ReasonBadPassword {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestAuthorizeValidationRejectsMissingFields(t *testing.T) {
	rec := postAuthorize(t, `{"username":"testuser"}`)
	if rec.Code != 422 {
		t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body.String())
	}
}

func TestAuthorizeValidationRejectsBadService(t *testing.T) {
	rec := postAuthorize(t, `{"username":"testuser","password":"x","nas_ip":"10.0.0.1","service":"carrier-pigeon"}`)
	if rec.Code != 422 {
		t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body.String())
	}
}
