// Package zaincash is the first live Iraqi e-wallet adapter (contract C3;
// sub-PRD 05 FR-23, OQ-1 default gateway). It follows ZainCash's published
// merchant integration pattern: an HS256 JWT carries the transaction request
// to POST /transaction/init, the customer is redirected to
// /transaction/pay?id=<txn>, and ZainCash calls back with another JWT
// (order id, its own transaction id, and a status) that this adapter
// verifies with the same merchant secret.
//
// SANDBOX NOTE (ship-what's-available, FR-23.5): HikRAD has no live ZainCash
// merchant account at the time this shipped. The JWT shapes and endpoints
// below follow ZainCash's public merchant documentation as best understood
// without one; they are NOT verified against a live/sandbox account. The
// adapter is registered but defaults to disabled (gateway_configs.enabled =
// false) so it can never appear as a payment option until an operator
// configures real credentials — per the phase brief, this is explicitly
// acceptable and the pending-credentials state is documented here rather than
// silently guessed at in the checklist. Re-verify every field name and
// endpoint against current ZainCash docs before enabling in production.
package zaincash

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hikrad/hikrad/internal/billing/gateways"
)

const (
	initURL = "https://api.zaincash.iq/transaction/init"
	payURL  = "https://api.zaincash.iq/transaction/pay"
	getURL  = "https://api.zaincash.iq/transaction/get"

	serviceType = "HikRAD renewal"
	tokenTTL    = 4 * time.Hour
)

// Config is the sealed merchant configuration (gateway_configs.creds_enc,
// decrypted by billing before constructing the adapter).
type Config struct {
	MerchantID     string `json:"merchant_id"`
	MerchantSecret string `json:"merchant_secret"`
	MSISDN         string `json:"msisdn"`       // merchant wallet phone number
	RedirectURL    string `json:"redirect_url"` // portal callback landing page
}

type Gateway struct {
	cfg Config
	hc  *http.Client
}

func New(cfg Config) *Gateway {
	return &Gateway{cfg: cfg, hc: &http.Client{Timeout: 15 * time.Second}}
}

func (g *Gateway) Name() string { return "zaincash" }

func (g *Gateway) CreatePayment(ctx context.Context, in gateways.Intent) (redirectURL, gatewayRef string, err error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"amount":      in.AmountIQD,
		"serviceType": serviceType,
		"msisdn":      g.cfg.MSISDN,
		"orderId":     in.ID, // echoed back verbatim in the callback (OrderID correlation)
		"redirectUrl": g.cfg.RedirectURL,
		"iat":         now.Unix(),
		"exp":         now.Add(tokenTTL).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(g.cfg.MerchantSecret))
	if err != nil {
		return "", "", err
	}

	form := url.Values{"token": {signed}, "merchantId": {g.cfg.MerchantID}, "lang": {"en"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, initURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.hc.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("zaincash: init request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("zaincash: init returned %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		ID  string `json:"id"`
		Err string `json:"err"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", "", fmt.Errorf("zaincash: decode init response: %w", err)
	}
	if out.ID == "" {
		return "", "", fmt.Errorf("zaincash: init error: %s", out.Err)
	}
	return payURL + "?id=" + url.QueryEscape(out.ID), out.ID, nil
}

// callbackClaims is what ZainCash's redirect/webhook token decodes to.
type callbackClaims struct {
	Status    string `json:"status"`
	OrderID   string `json:"orderid"`
	ID        string `json:"id"` // ZainCash's own transaction id (== gatewayRef)
	IQDAmount int64  `json:"iqAmount"`
}

var errBadSignature = errors.New("zaincash: invalid callback token")

func (g *Gateway) VerifyCallback(_ context.Context, r *http.Request) (gateways.CallbackResult, error) {
	raw := r.URL.Query().Get("token")
	if raw == "" {
		if err := r.ParseForm(); err == nil {
			raw = r.PostFormValue("token")
		}
	}
	if raw == "" {
		return gateways.CallbackResult{}, errors.New("zaincash: missing token")
	}
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return []byte(g.cfg.MerchantSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}))
	if err != nil {
		return gateways.CallbackResult{}, errBadSignature
	}
	status, _ := claims["status"].(string)
	orderID, _ := claims["orderid"].(string)
	txnID, _ := claims["id"].(string)
	if orderID == "" || txnID == "" {
		return gateways.CallbackResult{}, errBadSignature
	}
	state := gateways.StateFailed
	if status == "success" {
		state = gateways.StateConfirmed
	}
	// ZainCash's callback does not echo the amount back on every integration
	// version; when absent, billing's amount cross-check is skipped (0 means
	// "not reported") and the intent's own recorded amount is authoritative —
	// deliberately never trusted from an unsigned field.
	amount, _ := claims["iqAmount"].(float64)
	return gateways.CallbackResult{OrderID: orderID, GatewayRef: txnID, State: state, AmountIQD: int64(amount)}, nil
}

func (g *Gateway) QueryStatus(ctx context.Context, gatewayRef string) (gateways.State, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"id":     gatewayRef,
		"msisdn": g.cfg.MSISDN,
		"iat":    now.Unix(),
		"exp":    now.Add(tokenTTL).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(g.cfg.MerchantSecret))
	if err != nil {
		return "", err
	}
	form := url.Values{"token": {signed}, "merchantId": {g.cfg.MerchantID}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, getURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("zaincash: status request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var out struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("zaincash: decode status response: %w", err)
	}
	switch out.Status {
	case "success":
		return gateways.StateConfirmed, nil
	case "failed":
		return gateways.StateFailed, nil
	default:
		return gateways.StatePending, nil
	}
}
