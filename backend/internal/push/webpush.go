package push

// Standard Web Push (RFC 8291 message encryption + RFC 8292 VAPID), no
// third-party push service (NFR-7: degrades gracefully offline — a failed
// send is logged and the caller isolates it like every other alert channel).

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/crypto/hkdf"
)

// Payload is the frozen C4 shape: keys + params only, never rendered text —
// the client (F's PWA) localizes title_key/body_key with params client-side.
type Payload struct {
	TitleKey string         `json:"title_key"`
	BodyKey  string         `json:"body_key"`
	Params   map[string]any `json:"params,omitempty"`
	URL      string         `json:"url,omitempty"`
}

// maxPayloadBytes is the RFC 8291 practical ceiling most push services
// enforce (edge case in the task brief). encodeCapped drops Params first
// (keeping the routing-critical title/body/url) rather than truncating JSON,
// which would produce invalid output.
const maxPayloadBytes = 4096

func encodeCapped(p Payload) []byte {
	raw, _ := json.Marshal(p)
	if len(raw) <= maxPayloadBytes {
		return raw
	}
	p.Params = nil
	raw, _ = json.Marshal(p)
	return raw
}

// ErrGone means the push service reports the endpoint no longer exists (410
// Gone / 404 Not Found) — the caller should prune the subscription.
var ErrGone = fmt.Errorf("push: subscription gone")

// httpClient bounds every send so a hung push service can never stall a
// dispatch (mirrors monitorsvc's channel senders).
var httpClient = &http.Client{Timeout: 8 * time.Second}

// send delivers one encrypted push message to sub, signing with the VAPID
// identity. Returns ErrGone (caller prunes) on 404/410, another error on any
// other non-2xx/network failure, nil on success.
func send(ctx context.Context, sub Subscription, payload Payload) error {
	priv, pubB64, err := EnsureKeys(ctx, pkgSettings)
	if err != nil {
		return err
	}
	body, err := encryptPayload(encodeCapped(payload), sub.P256dh, sub.Auth)
	if err != nil {
		return fmt.Errorf("push: encrypt: %w", err)
	}
	aud, err := originOf(sub.Endpoint)
	if err != nil {
		return fmt.Errorf("push: bad endpoint: %w", err)
	}
	auth, err := vapidAuthHeader(priv, pubB64, aud)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", "86400") // 24h: generous for an offline device, bounded per NFR-7
	req.Header.Set("Authorization", auth)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrGone
	case resp.StatusCode >= 300:
		return fmt.Errorf("push: endpoint returned %d", resp.StatusCode)
	}
	return nil
}

func originOf(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid endpoint URL")
	}
	return u.Scheme + "://" + u.Host, nil
}

// --- RFC 8291 aes128gcm content encoding ------------------------------------

// encryptPayload seals plaintext for one subscriber, using a fresh ephemeral
// ECDH keypair per message (forward secrecy — the persistent VAPID identity
// key is unrelated and only ever used for JWT signing).
func encryptPayload(plaintext []byte, p256dhB64, authB64 string) ([]byte, error) {
	uaPubBytes, err := base64.RawURLEncoding.DecodeString(p256dhB64)
	if err != nil {
		return nil, fmt.Errorf("bad p256dh: %w", err)
	}
	authSecret, err := base64.RawURLEncoding.DecodeString(authB64)
	if err != nil {
		return nil, fmt.Errorf("bad auth secret: %w", err)
	}

	curve := ecdh.P256()
	uaPub, err := curve.NewPublicKey(uaPubBytes)
	if err != nil {
		return nil, fmt.Errorf("bad p256dh point: %w", err)
	}
	asPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	asPubBytes := asPriv.PublicKey().Bytes()

	ecdhSecret, err := asPriv.ECDH(uaPub)
	if err != nil {
		return nil, fmt.Errorf("ecdh: %w", err)
	}

	// IKM = HKDF-Expand(HKDF-Extract(salt=auth_secret, ikm=ecdh_secret), authInfo, 32)
	authInfo := append([]byte("WebPush: info\x00"), uaPubBytes...)
	authInfo = append(authInfo, asPubBytes...)
	ikm := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, ecdhSecret, authSecret, authInfo), ikm); err != nil {
		return nil, err
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
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
	// Single-record message: plaintext || 0x02 delimiter (RFC 8188 last record).
	padded := append(append([]byte{}, plaintext...), 0x02)
	ciphertext := gcm.Seal(nil, nonce, padded, nil)

	rs := make([]byte, 4)
	binary.BigEndian.PutUint32(rs, 4096)
	header := make([]byte, 0, 16+4+1+len(asPubBytes))
	header = append(header, salt...)
	header = append(header, rs...)
	header = append(header, byte(len(asPubBytes)))
	header = append(header, asPubBytes...)

	return append(header, ciphertext...), nil
}
