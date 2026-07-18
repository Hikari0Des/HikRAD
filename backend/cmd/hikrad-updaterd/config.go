package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// config is resolved once at boot from the environment (FR-86.1) — the same
// .env file install.sh writes and hikrad-api's compose environment reads, so
// the daemon and the panel relay always agree on the shared token with no
// separate provisioning step.
type config struct {
	Token        string // HIKRAD_UPDATER_TOKEN — required, refuses every request without it (FR-86.2)
	SocketPath   string // HIKRAD_UPDATER_SOCKET — unix socket only, never TCP
	Root         string // HIKRAD_ROOT — install root; every other path is derived from it
	LockPath     string // $Root/data/updater/update.lock — read-only probe target (see lock_unix.go doc)
	StatePath    string // $Root/data/updater/state.json — durable across daemon restarts (C2)
	IncomingDir  string // $Root/incoming — the only directory a bundle_path may resolve inside (FR-88.1)
	VersionFile  string // HIKRAD_VERSION_FILE — read for "current_version" / post-run outcome comparison
	DeliveryMode string // HIKRAD_DELIVERY_MODE — echoed back by `check`, informational only
	UpdateCmd    string // HIKRAD_UPDATE_CMD — path to the `hikrad` CLI; overridable in tests
}

func loadConfig() (config, error) {
	token := os.Getenv("HIKRAD_UPDATER_TOKEN")
	if token == "" {
		return config{}, fmt.Errorf("hikrad-updaterd: HIKRAD_UPDATER_TOKEN is required")
	}

	root := os.Getenv("HIKRAD_ROOT")
	if root == "" {
		root = "/opt/hikrad"
	}

	socket := os.Getenv("HIKRAD_UPDATER_SOCKET")
	if socket == "" {
		socket = "/var/run/hikrad-updater/updater.sock"
	}

	versionFile := os.Getenv("HIKRAD_VERSION_FILE")
	if versionFile == "" {
		versionFile = filepath.Join(root, "release", "VERSION")
	}

	updateCmd := os.Getenv("HIKRAD_UPDATE_CMD")
	if updateCmd == "" {
		updateCmd = "hikrad"
	}

	deliveryMode := os.Getenv("HIKRAD_DELIVERY_MODE")
	if deliveryMode == "" {
		deliveryMode = "source"
	}

	dataDir := filepath.Join(root, "data", "updater")
	return config{
		Token:        token,
		SocketPath:   socket,
		Root:         root,
		LockPath:     filepath.Join(dataDir, "update.lock"),
		StatePath:    filepath.Join(dataDir, "state.json"),
		IncomingDir:  filepath.Join(root, "incoming"),
		VersionFile:  versionFile,
		DeliveryMode: deliveryMode,
		UpdateCmd:    updateCmd,
	}, nil
}
