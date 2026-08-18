package vendor

// FR-148 hotspot login page push for the MikroTik adapter. The page lives on
// router FLASH under /hotspot/<server-name>/ (or the profile's html-directory
// override) — RouterOS serves it directly at login time, so HikRAD holds no
// copy the router reads from; pushing is the only way a design change ever
// reaches a subscriber. This is also the one write in this phase that is
// hardest to verify without real hardware: RouterOS 7.x's API supports
// writing a text file's contents directly via "/file/add contents=...";
// RouterOS 6.x's API does not expose file-content writes at all and needs
// FTP/SFTP for this in practice, which is out of scope for the RouterOS API
// ROSConn seam this package uses everywhere else. Both paths are attempted
// so the plumbing (version tracking, staleness detection) is real and
// testable against a fake conn; ROS6 support is the documented gap the gate
// result calls out rather than silently mis-claiming coverage.

import "fmt"

// PushHotspotPage implements Adapter.PushHotspotPage.
func (a mikrotikAdapter) PushHotspotPage(conn ROSConn, rosServerName string, files map[string]string) error {
	if rosServerName == "" {
		return fmt.Errorf("mikrotik: push hotspot page: no server name")
	}
	dir := "hotspot/" + rosServerName
	for name, contents := range files {
		path := dir + "/" + name
		// /file/add with inline contents is the ROS7 API mechanism for
		// writing a text file to flash without FTP. A router that rejects
		// this sentence (ROS6, or a build with file-write disabled over the
		// API) surfaces the router's own error to the caller rather than a
		// generic failure — the FR-148 UI is expected to tell the operator
		// to use FTP for that router, a known ROS6 limitation.
		if _, err := conn.Write("/file/add", "=name="+path, "=contents="+contents); err != nil {
			// RouterOS reports "already have such entry" for an existing file;
			// retry as a content update via /file/set rather than failing the
			// whole push over a file that already exists from a previous push.
			rows, readErr := conn.Read("/file/print", "?name="+path)
			if readErr != nil || len(rows) == 0 {
				return fmt.Errorf("mikrotik: push hotspot page file %q: %w", name, err)
			}
			if _, setErr := conn.Write("/file/set", "=.id="+rows[0][".id"], "=contents="+contents); setErr != nil {
				return fmt.Errorf("mikrotik: update hotspot page file %q: %w", name, setErr)
			}
		}
	}
	return nil
}
