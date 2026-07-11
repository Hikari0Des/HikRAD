//go:build !windows

package monitorsvc

// Per-volume disk usage on the container's filesystem (FR-35). The paths to
// report come from HIKRAD_HEALTH_DISK_PATHS (comma-separated), defaulting to the
// data root — the health API can only see mounts inside its own container, which
// is honest about what it measures.

import (
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func diskUsageAll() []diskUsage {
	out := make([]diskUsage, 0, 4)
	for _, p := range diskPaths() {
		var st unix.Statfs_t
		if err := unix.Statfs(p, &st); err != nil {
			continue
		}
		bsize := uint64(st.Bsize)
		total := st.Blocks * bsize
		free := st.Bavail * bsize
		used := total - free
		var pct float64
		if total > 0 {
			pct = float64(used) / float64(total) * 100
		}
		out = append(out, diskUsage{Path: p, TotalBytes: total, UsedBytes: used, FreeBytes: free, UsedPercent: pct})
	}
	return out
}

func diskPaths() []string {
	if v := os.Getenv("HIKRAD_HEALTH_DISK_PATHS"); v != "" {
		var ps []string
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				ps = append(ps, p)
			}
		}
		if len(ps) > 0 {
			return ps
		}
	}
	return []string{"/"}
}
