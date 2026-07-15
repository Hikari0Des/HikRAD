package vendor

// Production RouterOS API transport for auto-setup (FR-56.2/56.4). This is
// the only file in the codebase that dials a RouterOS API port — the
// vendor-isolation grep (FR-17.1) covers it like every other Mikrotik-*
// concern. Wraps github.com/go-routeros/routeros, itself a thin, dependency-
// free client over the documented binary API protocol.

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	routeros "github.com/go-routeros/routeros/v3"
)

// DialROSTimeout is the connect+login budget for both preview (read-only) and
// apply (read + write) — generous enough for a LAN router, short enough that
// an unreachable/wrong-credentials NAS fails the request fast rather than
// hanging the panel.
const DialROSTimeout = 8 * time.Second

type rosConn struct{ c *routeros.Client }

// DialROS connects and logs in to a RouterOS device's API (8728/8729,
// plaintext or TLS depending on port convention — v1 ships plaintext API per
// the default api_port; TLS is a config-only change at the client call site
// if a future NAS record wants 8729). Login failure (wrong credentials) and
// connect failure (unreachable) both surface here, before any command runs.
func DialROS(ctx context.Context, host string, port int, user, password string) (ROSConn, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dctx, cancel := context.WithTimeout(ctx, DialROSTimeout)
	defer cancel()
	c, err := routeros.DialContext(dctx, addr, user, password)
	if err != nil {
		return nil, fmt.Errorf("routeros: connect %s: %w", addr, err)
	}
	return &rosConn{c: c}, nil
}

func (r *rosConn) Read(sentence ...string) ([]map[string]string, error) {
	reply, err := r.c.Run(sentence...)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]string, 0, len(reply.Re))
	for _, s := range reply.Re {
		out = append(out, s.Map)
	}
	return out, nil
}

func (r *rosConn) Write(sentence ...string) (map[string]string, error) {
	reply, err := r.c.Run(sentence...)
	if err != nil {
		return nil, err
	}
	if reply.Done != nil {
		return reply.Done.Map, nil
	}
	return nil, nil
}

func (r *rosConn) Close() error { return r.c.Close() }
