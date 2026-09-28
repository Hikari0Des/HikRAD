#!/usr/bin/env bash
# uninstall.sh — remove a HikRAD installation completely (item 12).
#
# Thin wrapper over `hikrad uninstall` so removal works even when
# /usr/local/bin/hikrad was never installed or has been deleted: it runs the
# checkout's own copy of the CLI with the same environment resolution
# (HIKRAD_META → /opt/hikrad/install.meta → dev checkout fallback).
#
#   sudo ./scripts/uninstall.sh [--yes] [--keep-data] [--purge]
#
# What it removes (v4-2, FR-119): every hikrad-* systemd unit, the nightly
# backup timer, the nginx site, the CLI wrapper, and (with consent) the data
# directory. Backups and hikrad.env (which holds the backup passphrase) are
# kept unless --purge. PostgreSQL/Redis/nginx/FreeRADIUS packages are never
# removed — they may serve other workloads.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec bash "$SCRIPT_DIR/hikrad" uninstall "$@"
