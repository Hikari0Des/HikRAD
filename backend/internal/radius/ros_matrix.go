package radius

// ROS-version enablement gate for auto-setup (contract C6, sub-PRD 02 §7.2
// amendment): "apply is disabled for a version until its matrix leg is
// green." The source of truth for which legs are green is
// docs/ops/ros-matrix.md; this is its code-side mirror, kept intentionally
// tiny (one map) so bumping it after a pilot hardware run is a one-line
// change with no schema/migration involved.

import "strings"

// rosMatrixValidated reports whether auto-setup APPLY is enabled for a
// RouterOS version string (as stored on nas.ros_version, e.g. "6.49", "7",
// "7.11"). PREVIEW is always available (read-only) regardless of this gate —
// only the write path (apply) is refused for an unvalidated version, per
// FR-56.2/C6.
func rosMatrixValidated(rosVersion string) bool {
	v := strings.TrimSpace(rosVersion)
	switch {
	case v == "":
		return false // unknown version: refuse to guess, use the copy-paste snippet
	case strings.HasPrefix(v, "6"):
		return true // ROS 6.49+ — see docs/ops/ros-matrix.md
	case strings.HasPrefix(v, "7"):
		return true // ROS 7.x — see docs/ops/ros-matrix.md
	default:
		return false
	}
}
