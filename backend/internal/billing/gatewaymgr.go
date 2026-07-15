package billing

// Gateway registry + per-gateway admin config (contract C3, FR-23.1). mock is
// always registered (it ships forever, C3) and stateful across calls, so it
// lives as a package singleton; zaincash (and any future adapter) is
// stateless per credential set, so it is rebuilt from the decrypted config on
// every resolve — a credential rotation via the admin PUT takes effect on the
// very next payment with no restart.

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/hikrad/hikrad/internal/billing/gateways"
	"github.com/hikrad/hikrad/internal/billing/gateways/mock"
	"github.com/hikrad/hikrad/internal/billing/gateways/zaincash"
	"github.com/hikrad/hikrad/internal/platform/crypto"
	"github.com/jackc/pgx/v5"
)

var mockGW = mock.New()

var (
	errGatewayDisabled = errors.New("billing: gateway not enabled")
	errUnknownGateway  = errors.New("billing: unknown gateway")
)

type gatewayConfigRow struct {
	Gateway  string
	Enabled  bool
	Mode     string
	CredsEnc []byte
	HasCreds bool
}

func (m *Module) loadGatewayConfig(ctx context.Context, name string) (gatewayConfigRow, bool, error) {
	var row gatewayConfigRow
	err := m.db.QueryRow(ctx,
		`SELECT gateway, enabled, mode, creds_enc FROM gateway_configs WHERE gateway = $1`, name).
		Scan(&row.Gateway, &row.Enabled, &row.Mode, &row.CredsEnc)
	if errors.Is(err, pgx.ErrNoRows) {
		return gatewayConfigRow{}, false, nil
	}
	if err != nil {
		return gatewayConfigRow{}, false, err
	}
	row.HasCreds = len(row.CredsEnc) > 0
	return row, true, nil
}

// resolveGateway returns the adapter for name, only when it is enabled — the
// portal never offers, and can never create a payment against, a disabled or
// unconfigured gateway.
func (m *Module) resolveGateway(ctx context.Context, name string) (gateways.PaymentGateway, error) {
	cfg, ok, err := m.loadGatewayConfig(ctx, name)
	if err != nil {
		return nil, err
	}
	if !ok || !cfg.Enabled {
		return nil, errGatewayDisabled
	}
	return m.buildGateway(name, cfg)
}

// resolveGatewayForCallback resolves an adapter for verifying an inbound
// callback even if the gateway has since been disabled (an in-flight payment
// must still be able to complete/fail cleanly).
func (m *Module) resolveGatewayForCallback(ctx context.Context, name string) (gateways.PaymentGateway, error) {
	cfg, ok, err := m.loadGatewayConfig(ctx, name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errUnknownGateway
	}
	return m.buildGateway(name, cfg)
}

func (m *Module) buildGateway(name string, cfg gatewayConfigRow) (gateways.PaymentGateway, error) {
	switch name {
	case "mock":
		return mockGW, nil
	case "zaincash":
		var zc zaincash.Config
		if cfg.HasCreds {
			plain, derr := crypto.Decrypt(cfg.CredsEnc)
			if derr != nil {
				return nil, derr
			}
			if err := json.Unmarshal(plain, &zc); err != nil {
				return nil, err
			}
		}
		return zaincash.New(zc), nil
	default:
		return nil, errUnknownGateway
	}
}

// listEnabledGateways is what the portal renewal screen offers (sub-PRD 07
// FR-42: "lists only gateways enabled in settings").
func (m *Module) listEnabledGateways(ctx context.Context) ([]string, error) {
	rows, err := m.db.Query(ctx, `SELECT gateway FROM gateway_configs WHERE enabled ORDER BY gateway`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// --- Admin config (settings API now; Phase-5 settings screen wires the UI) --

type gatewayConfigSummary struct {
	Gateway    string `json:"gateway"`
	Enabled    bool   `json:"enabled"`
	Mode       string `json:"mode"`
	Configured bool   `json:"configured"` // true when creds are set; creds themselves never round-trip
}

func (m *Module) listGatewayConfigs(ctx context.Context) ([]gatewayConfigSummary, error) {
	rows, err := m.db.Query(ctx, `SELECT gateway, enabled, mode, (creds_enc IS NOT NULL) FROM gateway_configs ORDER BY gateway`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []gatewayConfigSummary{}
	for rows.Next() {
		var s gatewayConfigSummary
		if err := rows.Scan(&s.Gateway, &s.Enabled, &s.Mode, &s.Configured); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// putGatewayConfig upserts a gateway's enable flag and (optionally) new
// credentials, sealed with A's crypto before they ever reach the database.
func (m *Module) putGatewayConfig(ctx context.Context, name string, enabled bool, mode string, creds map[string]any) error {
	var credsEnc []byte
	if len(creds) > 0 {
		plain, err := json.Marshal(creds)
		if err != nil {
			return err
		}
		enc, err := crypto.Encrypt(plain)
		if err != nil {
			return err
		}
		credsEnc = enc
	}
	if mode == "" {
		mode = "live"
	}
	if credsEnc != nil {
		_, err := m.db.Exec(ctx,
			`INSERT INTO gateway_configs (gateway, enabled, mode, creds_enc, updated_at)
			 VALUES ($1, $2, $3, $4, now())
			 ON CONFLICT (gateway) DO UPDATE SET enabled = $2, mode = $3, creds_enc = $4, updated_at = now()`,
			name, enabled, mode, credsEnc)
		return err
	}
	_, err := m.db.Exec(ctx,
		`INSERT INTO gateway_configs (gateway, enabled, mode, updated_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (gateway) DO UPDATE SET enabled = $2, mode = $3, updated_at = now()`,
		name, enabled, mode)
	return err
}
