package main

// ROS-matrix mode (docs/ops/ros-matrix.md §1/§7): runs the automatable
// scenario suite against a target FreeRADIUS + hikrad-api stack and prints a
// matrix-formatted report tagged with -ros-label, so a pilot bring-up run
// against a ROS 6.49-fronted stack and a separate run against a 7.x-fronted
// stack produce directly comparable, pasteable evidence for the ops doc.
//
// This wraps the existing smoke suite (PAP/CHAP accept + every reject
// reason) — the CoA-side scenarios (disconnect, rate-change, pool-move,
// storm) need a live subscriber + NAS + Redis and stay on their own modes
// (-mode enforce, -mode seed-session, -mode coa-listen) since they require
// scenario-specific setup no single flag set can express; this mode's exit
// code and report are what CI's harness-smoke job actually gates on.

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func runROSMatrix(addr string, secret []byte, nasIP, rosLabel string, timeout time.Duration) int {
	if rosLabel == "" {
		rosLabel = "unlabeled"
	}
	fmt.Printf("=== ROS matrix run: %s (target %s) ===\n", rosLabel, addr)

	var rows []string
	failures := runSmoke(context.Background(), addr, secret, nasIP, timeout, func(name string, ok bool, detail string) {
		status := "PASS"
		if !ok {
			status = "FAIL"
		}
		rows = append(rows, fmt.Sprintf("| %-28s | %-4s | %-4s | %s |", name, rosLabel, status, detail))
		fmt.Printf("[%s] %s: %s\n", status, name, detail)
	})

	fmt.Println("\n| scenario                     | ros  | res  | detail")
	fmt.Println("|------------------------------|------|------|--------")
	for _, r := range rows {
		fmt.Println(r)
	}

	fmt.Printf("\n%d/%d scenarios passed for ros=%s.\n", len(rows)-failures, len(rows), rosLabel)
	fmt.Println(strings.TrimSpace(`
Not covered by this automated run (needs a live subscriber + NAS + Redis —
see docs/ops/ros-matrix.md §5 for the manual checklist):
  - CoA Disconnect / rate-change / pool-move round trips  (-mode enforce, -mode seed-session)
  - Hotspot voucher login                                  (-mode voucher-login)
  - NAS API auto-setup preview/apply against this exact router
  - Walled-garden negative reachability
`))

	if failures > 0 {
		return 1
	}
	return 0
}
