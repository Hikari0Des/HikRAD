// Standalone TCP readiness probe compiled into the hikrad-api image and run
// by the compose healthcheck (contract C5: healthchecks on all services).
//
// A successful dial of :8080 is a real readiness signal, not just liveness:
// cmd/hikrad-api only starts listening after config load, boot migrations,
// and DB/Redis pings have all succeeded. If Agents A+D later contract a
// GET /healthz route in httpapi, this probe can be upgraded to an HTTP GET —
// swap the compose healthcheck line, nothing else.
//
// Not part of the backend Go module (Agent D's go.mod); built with
// `go mod init` inside deploy/docker/api.Dockerfile. Stdlib only.
package main

import (
	"net"
	"os"
	"time"
)

func main() {
	addr := "127.0.0.1:8080"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		os.Exit(1)
	}
	_ = conn.Close()
}
