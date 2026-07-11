//go:build windows

package monitorsvc

// Disk usage is a Linux-container concern (FR-35); on a Windows dev box the
// health endpoint simply reports no volumes rather than pulling in Win32 APIs.
// The production monitor image is Linux, where disk_unix.go applies.

func diskUsageAll() []diskUsage { return nil }
