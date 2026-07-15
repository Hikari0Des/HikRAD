// Package mock is the always-shipped PaymentGateway adapter (contract C3,
// FR-23.5): a full lifecycle e-wallet stand-in for CI, the gate, and F's
// portal development, with no external dependency. Its callback is
// HMAC-signed exactly like a real gateway's would be, so exercising it proves
// the whole signature-verify + idempotent-confirm + reconcile pipeline, not
// just a stub. A dev-only simulator (Simulate) drives state transitions
// (approve/fail/delay) the way a human tapping through a real wallet app
// would, without needing a browser.
package mock

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/hikrad/hikrad/internal/billing/gateways"
)

// devSecret signs the mock adapter's callback payloads. It is not a real
// secret (there is nothing to protect — this adapter never touches money) but
// keeping the HMAC step makes the mock exercise the same tamper/signature
// code paths a live adapter needs, so the "signature-verified" contract is
// actually tested rather than assumed.
var devSecret = []byte("hikrad-mock-gateway-dev-secret")

type order struct {
	gatewayRef string
	amountIQD  int64
	state      gateways.State
}

// Gateway is the in-memory mock adapter. State does not survive a process
// restart by design (it exists purely for CI/demo lifecycles that fit inside
// one process run).
type Gateway struct {
	mu     sync.RWMutex
	orders map[string]*order // keyed by intent (order) id
	refs   map[string]string // gatewayRef -> order id
}

func New() *Gateway {
	return &Gateway{orders: map[string]*order{}, refs: map[string]string{}}
}

func (g *Gateway) Name() string { return "mock" }

func (g *Gateway) CreatePayment(_ context.Context, in gateways.Intent) (redirectURL, gatewayRef string, err error) {
	ref, err := randRef()
	if err != nil {
		return "", "", err
	}
	g.mu.Lock()
	g.orders[in.ID] = &order{gatewayRef: ref, amountIQD: in.AmountIQD, state: gateways.StatePending}
	g.refs[ref] = in.ID
	g.mu.Unlock()
	return "https://mock.gateway.hikrad.local/pay/" + ref, ref, nil
}

type callbackPayload struct {
	OrderID    string `json:"order_id"`
	GatewayRef string `json:"gateway_ref"`
	AmountIQD  int64  `json:"amount_iqd"`
	State      string `json:"state"`
	Signature  string `json:"signature"`
}

// sign computes the HMAC-SHA256 hex signature over the payload fields (order
// matters — it is the canonical string every signer/verifier must agree on).
func sign(orderID, gatewayRef string, amountIQD int64, state gateways.State) string {
	mac := hmac.New(sha256.New, devSecret)
	fmt.Fprintf(mac, "%s|%s|%d|%s", orderID, gatewayRef, amountIQD, state)
	return hex.EncodeToString(mac.Sum(nil))
}

var errBadSignature = errors.New("mock: invalid callback signature")

func (g *Gateway) VerifyCallback(_ context.Context, r *http.Request) (gateways.CallbackResult, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		return gateways.CallbackResult{}, err
	}
	var p callbackPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return gateways.CallbackResult{}, err
	}
	want := sign(p.OrderID, p.GatewayRef, p.AmountIQD, gateways.State(p.State))
	if !hmac.Equal([]byte(want), []byte(p.Signature)) {
		return gateways.CallbackResult{}, errBadSignature
	}
	return gateways.CallbackResult{
		OrderID: p.OrderID, GatewayRef: p.GatewayRef,
		State: gateways.State(p.State), AmountIQD: p.AmountIQD,
	}, nil
}

func (g *Gateway) QueryStatus(_ context.Context, gatewayRef string) (gateways.State, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	id, ok := g.refs[gatewayRef]
	if !ok {
		return "", errors.New("mock: unknown gateway_ref")
	}
	o := g.orders[id]
	if o == nil {
		return "", errors.New("mock: unknown gateway_ref")
	}
	return o.state, nil
}

// Simulate drives the dev-only lifecycle control (approve/fail/delay) used by
// the gate, CI, and portal development. approve/fail build a fully signed
// callback payload identical in shape to what a live callback POST would
// carry, so a caller can feed it straight into VerifyCallback for an
// end-to-end exercise of the real pipeline; delay is a no-op observation point
// (state stays pending) for reconciliation-timing tests.
func (g *Gateway) Simulate(gatewayRef, action string) ([]byte, error) {
	g.mu.Lock()
	id, ok := g.refs[gatewayRef]
	if !ok {
		g.mu.Unlock()
		return nil, errors.New("mock: unknown gateway_ref")
	}
	o := g.orders[id]
	var state gateways.State
	switch action {
	case "approve":
		state = gateways.StateConfirmed
	case "fail":
		state = gateways.StateFailed
	case "delay":
		g.mu.Unlock()
		return nil, nil // observe-only; state unchanged
	default:
		g.mu.Unlock()
		return nil, fmt.Errorf("mock: unknown action %q", action)
	}
	o.state = state
	amount := o.amountIQD
	g.mu.Unlock()

	payload := callbackPayload{
		OrderID: id, GatewayRef: gatewayRef, AmountIQD: amount, State: string(state),
		Signature: sign(id, gatewayRef, amount, state),
	}
	return json.Marshal(payload)
}

func randRef() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "mock-" + hex.EncodeToString(b), nil
}
