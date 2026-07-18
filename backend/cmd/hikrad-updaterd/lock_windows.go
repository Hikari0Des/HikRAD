//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// probeLock is the Windows-dev-box counterpart of lock_unix.go's real flock
// probe (production is Linux-only per the systemd unit, same posture as
// internal/monitorsvc/disk_windows.go — but LockFileEx costs little extra
// and lets the daemon's own test suite run un-skipped on a Windows dev
// machine too, so it's implemented for real rather than stubbed to a
// constant).
func probeLock(path string) (held bool, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()

	ol := new(windows.Overlapped)
	lockErr := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_FAIL_IMMEDIATELY|windows.LOCKFILE_EXCLUSIVE_LOCK,
		0, 1, 0, ol,
	)
	if lockErr != nil {
		if lockErr == windows.ERROR_LOCK_VIOLATION {
			return true, nil
		}
		return false, lockErr
	}
	ulOl := new(windows.Overlapped)
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ulOl)
	return false, nil
}
