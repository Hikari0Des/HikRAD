//go:build !windows

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// probeLock is a best-effort, read-only signal for `status`/reconciliation —
// NOT the enforcement mechanism (that's scripts/hikrad's own flock, held by
// the `hikrad update` child process itself, C3). It answers "does anything
// currently hold this lock file" without blocking, and always releases
// immediately if it acquires: a status check must never itself become the
// thing holding the lock.
func probeLock(path string) (held bool, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()

	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if err == unix.EWOULDBLOCK {
			return true, nil
		}
		return false, err
	}
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
	return false, nil
}
