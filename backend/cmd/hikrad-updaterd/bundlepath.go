package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// bundleFileRe matches both a bare filename ("hikrad-v2.7.0.tar", used by
// findNewerBundle's directory scan) and captures the version components for
// reuse by the target-version seam in run.go.
var bundleFileRe = regexp.MustCompile(`^hikrad-v([0-9]+)\.([0-9]+)\.([0-9]+)\.tar$`)

// validateBundlePath enforces FR-88.1's "no argument ever reaches a shell"
// boundary for the one field of the protocol that names a filesystem path:
// bundle_path must resolve, after Clean, to a path directly inside
// $HIKRAD_ROOT/incoming/, with a basename matching the exact bundle filename
// shape — never a directory, never a traversal, never anything exec.Command
// could be tricked into treating as a flag or shell metacharacter (the
// filename regex alone already excludes every shell-special character).
func validateBundlePath(root, p string) (string, error) {
	if p == "" {
		return "", nil
	}
	clean := filepath.Clean(p)
	incoming := filepath.Clean(filepath.Join(root, "incoming"))

	rel, err := filepath.Rel(incoming, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == "." {
		return "", fmt.Errorf("invalid bundle_path")
	}
	// Reject any path that isn't a direct child of incoming/ (no subdirectories).
	if strings.ContainsRune(rel, filepath.Separator) {
		return "", fmt.Errorf("invalid bundle_path")
	}
	if !bundleFileRe.MatchString(filepath.Base(clean)) {
		return "", fmt.Errorf("invalid bundle_path")
	}
	return clean, nil
}

// bundleTargetVersion best-effort parses "hikrad-vX.Y.Z.tar" -> "vX.Y.Z" for
// the reconciliation seam (state.go's TargetVersion); empty for anything that
// doesn't match (registry/source-mode updates have no bundle filename at all).
func bundleTargetVersion(bundlePath string) string {
	m := bundleFileRe.FindStringSubmatch(filepath.Base(bundlePath))
	if m == nil {
		return ""
	}
	return "v" + m[1] + "." + m[2] + "." + m[3]
}
