#!/usr/bin/env bash
# Self-test for gen-env.sh (Agent A definition of done: "gen-env produces
# valid unique secrets twice"). Run directly or via `make test`.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GEN="$SCRIPT_DIR/gen-env.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail() { echo "FAIL: $1" >&2; exit 1; }

get() { # get FILE KEY -> value
    grep "^$2=" "$1" | head -n1 | cut -d= -f2-
}

"$GEN" "$TMP/a.env" >/dev/null
"$GEN" --env prod "$TMP/b.env" >/dev/null

for f in "$TMP/a.env" "$TMP/b.env"; do
    [ -f "$f" ] || fail "$f was not created"
    for key in POSTGRES_PASSWORD HIKRAD_DB_URL HIKRAD_REDIS_URL \
               HIKRAD_ENCRYPTION_KEY HIKRAD_JWT_SECRET HIKRAD_ENV \
               HIKRAD_BACKUP_PASSPHRASE HIKRAD_BACKUP_RETENTION; do
        v="$(get "$f" "$key")"
        [ -n "$v" ] || fail "$key empty in $f"
        case "$v" in *CHANGE_ME*) fail "$key still a placeholder in $f" ;; esac
    done
    # Encryption key must decode to exactly 32 bytes (AES-256).
    n="$(get "$f" HIKRAD_ENCRYPTION_KEY | openssl base64 -d -A | wc -c | tr -d ' ')"
    [ "$n" = "32" ] || fail "HIKRAD_ENCRYPTION_KEY in $f decodes to $n bytes, want 32"
done

# Secrets must be unique across runs.
for key in POSTGRES_PASSWORD HIKRAD_ENCRYPTION_KEY HIKRAD_JWT_SECRET HIKRAD_BACKUP_PASSPHRASE; do
    [ "$(get "$TMP/a.env" "$key")" != "$(get "$TMP/b.env" "$key")" ] \
        || fail "$key identical across two runs"
done

# --env flag honored.
[ "$(get "$TMP/a.env" HIKRAD_ENV)" = "dev" ] || fail "default HIKRAD_ENV should be dev"
[ "$(get "$TMP/b.env" HIKRAD_ENV)" = "prod" ] || fail "--env prod not honored"

# Refuses to overwrite without --force; overwrites with it.
if "$GEN" "$TMP/a.env" >/dev/null 2>&1; then
    fail "second run onto the same path should refuse without --force"
fi
before="$(get "$TMP/a.env" HIKRAD_JWT_SECRET)"
"$GEN" --force "$TMP/a.env" >/dev/null
[ "$(get "$TMP/a.env" HIKRAD_JWT_SECRET)" != "$before" ] || fail "--force did not regenerate"

echo "gen-env.test.sh: all checks passed"
