package vendor

// FR-147 RouterOS Update tab for the MikroTik adapter. Read/check are pure
// print sentences; ApplyRouterUpdate is the phase's other genuinely
// destructive write (besides Reboot) — it does not go through the plan/
// preview/apply pipeline (there is nothing to "diff": either an update is
// available and gets installed, or it doesn't) but is gated exactly like
// Reboot by its caller: explicit double-confirm, permission, audit, and the
// "device will reboot and sessions will drop" warning.

import "fmt"

// ReadUpdateInfo implements Adapter.ReadUpdateInfo.
func (a mikrotikAdapter) ReadUpdateInfo(conn ROSConn) (RouterUpdateInfo, error) {
	var info RouterUpdateInfo
	rows, err := conn.Read("/system/package/update/print")
	if err != nil {
		return info, fmt.Errorf("mikrotik: read /system/package/update: %w", err)
	}
	if len(rows) > 0 {
		info.Channel = rows[0]["channel"]
		info.InstalledVersion = rows[0]["installed-version"]
		info.LatestVersion = rows[0]["latest-version"]
		info.UpdateAvailable = info.LatestVersion != "" && info.LatestVersion != info.InstalledVersion
	}
	pkgRows, err := conn.Read("/system/package/print")
	if err != nil {
		return info, fmt.Errorf("mikrotik: read /system/package: %w", err)
	}
	for _, r := range pkgRows {
		info.Packages = append(info.Packages, RouterPackage{
			Name: r["name"], Version: r["version"], Enabled: r["disabled"] != "true",
		})
	}
	return info, nil
}

// CheckForUpdates implements Adapter.CheckForUpdates: triggers the router's
// own update check, then re-reads (RouterOS's check-for-updates is a
// fire-and-poll command; ReadUpdateInfo's print reflects the result once the
// router has finished contacting the update server).
func (a mikrotikAdapter) CheckForUpdates(conn ROSConn) (RouterUpdateInfo, error) {
	if _, err := conn.Write("/system/package/update/check-for-updates"); err != nil {
		return RouterUpdateInfo{}, fmt.Errorf("mikrotik: check-for-updates: %w", err)
	}
	return a.ReadUpdateInfo(conn)
}

// ApplyRouterUpdate implements Adapter.ApplyRouterUpdate.
func (a mikrotikAdapter) ApplyRouterUpdate(conn ROSConn, channel string) error {
	if channel != "" {
		if _, err := conn.Write("/system/package/update/set", "=channel="+channel); err != nil {
			return fmt.Errorf("mikrotik: set update channel: %w", err)
		}
	}
	if _, err := conn.Write("/system/package/update/install"); err != nil {
		return fmt.Errorf("mikrotik: install update: %w", err)
	}
	return nil
}
