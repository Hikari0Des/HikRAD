package push

// Unit tests requiring no DB/Redis: VAPID bootstrap idempotence, RFC 8291
// payload encryption round-trip, 4 KB payload capping, and delivery/pruning
// isolation against an httptest fake (gate item 9's "request-capture fake"
// fallback pattern, reused here for push since Web Push needs the same proof
// style whenever a live browser subscription isn't available in CI).

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/platform/crypto"
	"golang.org/x/crypto/hkdf"
)

func init() {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 3)
	}
	if err := crypto.Configure(key); err != nil {
		panic(err)
	}
}

// fakeSettings is a minimal in-memory platform.Settings for tests that don't
// need a real DB.
type fakeSettings struct {
	mu sync.Mutex
	m  map[string]json.RawMessage
}

func newFakeSettings() *fakeSettings { return &fakeSettings{m: map[string]json.RawMessage{}} }

func (f *fakeSettings) GetRaw(_ context.Context, key string) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.m[key]
	if !ok {
		return nil, platform.ErrSettingNotFound
	}
	return v, nil
}

func (f *fakeSettings) Set(_ context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.m[key] = raw
	f.mu.Unlock()
	return nil
}

func (f *fakeSettings) Invalidate(key string) {
	f.mu.Lock()
	delete(f.m, key)
	f.mu.Unlock()
}

func (f *fakeSettings) OnChange(func(string)) {}

func TestEnsureKeys_Idempotent(t *testing.T) {
	resetCache()
	settings := newFakeSettings()
	ctx := context.Background()

	_, pub1, err := EnsureKeys(ctx, settings)
	if err != nil {
		t.Fatalf("EnsureKeys (first): %v", err)
	}
	if pub1 == "" {
		t.Fatal("expected a non-empty public key")
	}

	// Simulate a fresh process: drop the in-process cache but keep the same
	// backing settings store — EnsureKeys must reuse the persisted key rather
	// than generating a new one.
	resetCache()
	_, pub2, err := EnsureKeys(ctx, settings)
	if err != nil {
		t.Fatalf("EnsureKeys (second): %v", err)
	}
	if pub1 != pub2 {
		t.Fatalf("VAPID public key changed across bootstraps: %q != %q (idempotence broken)", pub1, pub2)
	}

	// A third call without resetting the cache must also return the same key
	// (in-process fast path).
	_, pub3, err := EnsureKeys(ctx, settings)
	if err != nil {
		t.Fatalf("EnsureKeys (third): %v", err)
	}
	if pub3 != pub1 {
		t.Fatalf("cached EnsureKeys call returned a different key: %q", pub3)
	}
	resetCache()
}

func TestVapidAuthHeader_Shape(t *testing.T) {
	resetCache()
	settings := newFakeSettings()
	priv, pub, err := EnsureKeys(context.Background(), settings)
	if err != nil {
		t.Fatalf("EnsureKeys: %v", err)
	}
	hdr, err := vapidAuthHeader(priv, pub, "https://push.example.com")
	if err != nil {
		t.Fatalf("vapidAuthHeader: %v", err)
	}
	if !strings.HasPrefix(hdr, "vapid t=") || !strings.Contains(hdr, ", k=") {
		t.Fatalf("unexpected VAPID header shape: %q", hdr)
	}
	resetCache()
}

// TestEncryptPayload_RoundTrip validates the RFC 8291 aes128gcm implementation
// end-to-end by decrypting with an independent implementation of the
// receiving (browser/push-service) side, mirroring the spec exactly.
func TestEncryptPayload_RoundTrip(t *testing.T) {
	curve := ecdh.P256()
	uaPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	uaPubB64 := base64.RawURLEncoding.EncodeToString(uaPriv.PublicKey().Bytes())
	authSecret := make([]byte, 16)
	_, _ = rand.Read(authSecret)
	authB64 := base64.RawURLEncoding.EncodeToString(authSecret)

	plaintext := []byte(`{"title_key":"push.alert.disk_low.title","body_key":"push.alert.disk_low.body"}`)
	wire, err := encryptPayload(plaintext, uaPubB64, authB64)
	if err != nil {
		t.Fatalf("encryptPayload: %v", err)
	}

	got, err := decryptForTest(wire, uaPriv, authSecret)
	if err != nil {
		t.Fatalf("decryptForTest: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("round-trip mismatch: got %q want %q", got, plaintext)
	}
}

// decryptForTest is an independent aes128gcm reader (the receiving side of
// RFC 8291) used only to prove encryptPayload is spec-correct.
func decryptForTest(wire []byte, uaPriv *ecdh.PrivateKey, authSecret []byte) ([]byte, error) {
	if len(wire) < 16+4+1 {
		return nil, errors.New("short header")
	}
	salt := wire[:16]
	rs := binary.BigEndian.Uint32(wire[16:20])
	idlen := int(wire[20])
	if len(wire) < 21+idlen {
		return nil, errors.New("short keyid")
	}
	asPubBytes := wire[21 : 21+idlen]
	ciphertext := wire[21+idlen:]
	_ = rs

	curve := ecdh.P256()
	asPub, err := curve.NewPublicKey(asPubBytes)
	if err != nil {
		return nil, err
	}
	ecdhSecret, err := uaPriv.ECDH(asPub)
	if err != nil {
		return nil, err
	}
	authInfo := append([]byte("WebPush: info\x00"), uaPriv.PublicKey().Bytes()...)
	authInfo = append(authInfo, asPubBytes...)
	ikm := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, ecdhSecret, authSecret, authInfo), ikm); err != nil {
		return nil, err
	}
	cek := make([]byte, 16)
	if _, err := io.ReadFull(hkdf.New(sha256.New, ikm, salt, []byte("Content-Encoding: aes128gcm\x00")), cek); err != nil {
		return nil, err
	}
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(hkdf.New(sha256.New, ikm, salt, []byte("Content-Encoding: nonce\x00")), nonce); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	padded, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	// Strip the RFC 8188 last-record delimiter (0x02) + any zero padding after it.
	end := len(padded)
	for end > 0 && padded[end-1] == 0 {
		end--
	}
	if end == 0 || padded[end-1] != 0x02 {
		return nil, errors.New("bad padding delimiter")
	}
	return padded[:end-1], nil
}

func TestEncodeCapped_DropsParamsOverLimit(t *testing.T) {
	huge := make(map[string]any, 200)
	for i := 0; i < 200; i++ {
		huge[fmt.Sprintf("param_%03d", i)] = strings.Repeat("x", 100)
	}
	p := Payload{TitleKey: "t", BodyKey: "b", URL: "/u", Params: huge}
	raw := encodeCapped(p)
	if len(raw) > maxPayloadBytes {
		t.Fatalf("encodeCapped exceeded the cap: %d bytes", len(raw))
	}
	var decoded Payload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("capped payload is not valid JSON: %v", err)
	}
	if decoded.TitleKey != "t" || decoded.BodyKey != "b" || decoded.URL != "/u" {
		t.Fatalf("capping dropped routing-critical fields: %+v", decoded)
	}
	if len(decoded.Params) != 0 {
		t.Fatalf("expected Params dropped once over the cap, got %d entries", len(decoded.Params))
	}
}

// TestSend_PrunesOnGone proves the 410/404 pruning path against a
// request-capture fake HTTP endpoint (no live browser needed in CI).
func TestSend_PrunesOnGone(t *testing.T) {
	resetCache()
	pkgSettings = newFakeSettings()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer srv.Close()

	curve := ecdh.P256()
	uaPriv, _ := curve.GenerateKey(rand.Reader)
	authSecret := make([]byte, 16)
	_, _ = rand.Read(authSecret)
	sub := Subscription{
		Endpoint: srv.URL + "/some-endpoint-token",
		P256dh:   base64.RawURLEncoding.EncodeToString(uaPriv.PublicKey().Bytes()),
		Auth:     base64.RawURLEncoding.EncodeToString(authSecret),
	}
	err := send(context.Background(), sub, Payload{TitleKey: "t", BodyKey: "b"})
	if !errors.Is(err, ErrGone) {
		t.Fatalf("expected ErrGone, got %v", err)
	}
	resetCache()
}

// TestDeliverAll_Isolation proves a failing subscription never stops delivery
// to the others (NFR-7 delivery isolation). Uses a 500 (not 410) so this
// stays DB-free — the 410-prunes-for-real path is covered by the DB-backed
// TestDeliverAll_PrunesGoneSubscription in store_test.go.
func TestDeliverAll_Isolation(t *testing.T) {
	resetCache()
	pkgSettings = newFakeSettings()

	var goodHits, failHits int
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		goodHits++
		w.WriteHeader(http.StatusCreated)
	}))
	defer good.Close()
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failHits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()

	mk := func(base string) Subscription {
		curve := ecdh.P256()
		p, _ := curve.GenerateKey(rand.Reader)
		a := make([]byte, 16)
		_, _ = rand.Read(a)
		return Subscription{
			Endpoint: base + "/endpoint-token",
			P256dh:   base64.RawURLEncoding.EncodeToString(p.PublicKey().Bytes()),
			Auth:     base64.RawURLEncoding.EncodeToString(a),
		}
	}
	subs := []Subscription{mk(good.URL), mk(failing.URL)}
	if err := deliverAll(context.Background(), subs, Payload{TitleKey: "t", BodyKey: "b"}); err != nil {
		t.Fatalf("deliverAll should succeed when at least one endpoint is healthy: %v", err)
	}
	if goodHits != 1 {
		t.Fatalf("expected the healthy endpoint to receive 1 request, got %d", goodHits)
	}
	if failHits != 1 {
		t.Fatalf("expected the failing endpoint to still be attempted once, got %d", failHits)
	}

	// All-failing must surface as an error to the caller (the alert
	// dispatcher's per-channel retry then applies).
	subs2 := []Subscription{mk(failing.URL)}
	if err := deliverAll(context.Background(), subs2, Payload{TitleKey: "t", BodyKey: "b"}); err == nil {
		t.Fatal("expected an error when every subscription failed")
	}
	resetCache()
}

// DeliverPanel/DeliverToSubscriber with zero subscriptions must be a no-op
// success, not a failure — nothing has installed the PWA yet (NFR-7).
func TestDeliverAll_NoSubscriptions(t *testing.T) {
	if err := deliverAll(context.Background(), nil, Payload{TitleKey: "t", BodyKey: "b"}); err != nil {
		t.Fatalf("expected nil for zero subscriptions, got %v", err)
	}
}
