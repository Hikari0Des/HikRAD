#!/bin/sh
# HikRAD FreeRADIUS supervisor (FR-13.2/AC-13a).
#
# FreeRADIUS 3.x reads its client list (clients.conf -> clients-generated.conf)
# only ONCE, at startup. Neither SIGHUP nor a control-socket "reload" re-reads
# the clients or listen sections — that is a documented FreeRADIUS limitation,
# confirmed live against 3.2.3. So a NAS added or edited through the panel,
# which makes hikrad-api rewrite clients-generated.conf, does NOT become a
# recognized RADIUS client until the server is restarted; until then its packets
# are dropped as "unknown client" and the NAS reports a RADIUS timeout.
#
# This supervisor runs FreeRADIUS in the foreground and restarts it whenever
# clients-generated.conf changes, so that recognition happens automatically with
# no operator action and no per-install manual step. NAS changes are rare,
# admin-initiated events; the sub-second restart window is covered by normal
# RADIUS retransmission, so steady-state accounting (the M2 lossless claim) is
# unaffected. It also restarts FreeRADIUS if it exits on its own, so a transient
# failure self-heals instead of taking RADIUS down.
set -u

CLIENTS="${HIKRAD_CLIENTS_FILE:-/etc/raddb/clients-generated.conf}"
POLL="${HIKRAD_CLIENTS_POLL_SECONDS:-3}"
PID=""

# The Debian FreeRADIUS package names the daemon `freeradius`; resolve it by
# name but fall back to the known install path so the supervisor works whether
# or not /usr/sbin is on PATH (it is under the image's own entrypoint, but not
# necessarily when this script is invoked directly).
RADIUSD="$(command -v freeradius 2>/dev/null || command -v radiusd 2>/dev/null || echo /usr/sbin/freeradius)"

fingerprint() { md5sum "$CLIENTS" 2>/dev/null | awk '{print $1}'; }

shutdown() {
	[ -n "$PID" ] && kill -TERM "$PID" 2>/dev/null
	wait "$PID" 2>/dev/null
	exit 0
}
trap shutdown TERM INT

while true; do
	# -f: foreground. -l stdout: send the server log to stdout so
	# `docker logs hikrad-freeradius` shows startup ("Listening on ...",
	# "Ready to process requests"), unknown-client drops, and errors. (The
	# radiusd.conf `destination = stdout` alone does not emit here; the -l flag
	# is what actually routes the log to the container's stdout.) This is INFO
	# level, not the full -X packet trace — operational visibility without the
	# volume or the risk of logging credentials.
	"$RADIUSD" -f -l stdout &
	PID=$!
	seen=$(fingerprint)

	while kill -0 "$PID" 2>/dev/null; do
		sleep "$POLL"
		now=$(fingerprint)
		if [ "$now" != "$seen" ]; then
			echo "hikrad-run: clients-generated.conf changed -> restarting FreeRADIUS to load the new NAS client list"
			kill -TERM "$PID" 2>/dev/null
			wait "$PID" 2>/dev/null
			break
		fi
	done

	# Reap (no-op if already reaped above) and loop. Brief pause so a FreeRADIUS
	# that keeps failing to start does not hot-loop.
	wait "$PID" 2>/dev/null
	sleep 1
done
