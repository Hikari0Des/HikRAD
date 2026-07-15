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
# What it removes: the compose stack + its images, the nightly backup cron
# entry, the CLI wrapper, and (with consent) the data directory. Backups and
# .env (which holds the backup passphrase) are kept unless --purge. Docker
# Engine is never removed.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec bash "$SCRIPT_DIR/hikrad" uninstall "$@"
