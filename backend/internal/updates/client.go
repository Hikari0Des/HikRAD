package updates

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

var errNotConfigured = errors.New("hikrad-updaterd is not configured for this install")

// dial opens a fresh connection to the daemon's unix socket. Every verb gets
// its own connection (the daemon's own protocol, C2: one request per
// connection) — `update` is the one exception that keeps its connection open
// past the first response line (see relay.go).
func dial() (net.Conn, error) {
	if !configured() {
		return nil, errNotConfigured
	}
	conn, err := net.DialTimeout("unix", socketPath(), 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial hikrad-updaterd: %w", err)
	}
	return conn, nil
}

func sendRequest(conn net.Conn, req request) error {
	req.Token = authToken()
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err = conn.Write(b)
	return err
}

// readOneJSON reads exactly one newline-delimited JSON line, matching the
// daemon's own framing (C2), and decodes it into v.
func readOneJSON(r *bufio.Reader, v any) error {
	line, err := r.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return err
	}
	return json.Unmarshal([]byte(strings.TrimSpace(line)), v)
}

// oneShot performs a single request/response round trip for the three
// non-streaming verbs (check/status/rollback-status) and decodes the
// daemon's one response line into v. Returns errNotConfigured verbatim so
// handlers can distinguish "not provisioned" from "provisioned but
// unreachable" (both surfaced as 503, but with different messages, C4).
func oneShot(verb string, v any) error {
	conn, err := dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := sendRequest(conn, request{Verb: verb}); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)
	return readOneJSON(r, v)
}
