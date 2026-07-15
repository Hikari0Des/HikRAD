package vendor

import "fmt"

// DeviceInfo is what ReadDeviceInfo learns from a router in one read-only API
// session (FR-13/item 8: auto-fill ros_version when adding a NAS).
type DeviceInfo struct {
	Version   string // e.g. "7.14.2"
	BoardName string // e.g. "CCR2004-1G-12S+2XS"
	Identity  string // /system identity name
}

// ReadDeviceInfo reads the RouterOS version, board name and identity over an
// already-connected API session. Read-only (print sentences only) — safe to
// run against a production router. Lives in this package because the sentences
// are RouterOS-specific (FR-17 vendor isolation).
func ReadDeviceInfo(conn ROSConn) (DeviceInfo, error) {
	var info DeviceInfo
	rows, err := conn.Read("/system/resource/print")
	if err != nil {
		return info, fmt.Errorf("read /system/resource: %w", err)
	}
	if len(rows) > 0 {
		info.Version = rows[0]["version"]
		info.BoardName = rows[0]["board-name"]
	}
	// Identity is cosmetic; a failure here must not fail the probe.
	if idRows, err := conn.Read("/system/identity/print"); err == nil && len(idRows) > 0 {
		info.Identity = idRows[0]["name"]
	}
	if info.Version == "" {
		return info, fmt.Errorf("router reported no version")
	}
	return info, nil
}
