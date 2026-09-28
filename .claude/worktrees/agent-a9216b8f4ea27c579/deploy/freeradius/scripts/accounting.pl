#!/usr/bin/perl
# HikRAD FreeRADIUS -> hikrad-acct accounting forward (Phase 2, Agent 2 / RADIUS
# & NAS). Owned config; the ingest endpoint's semantics are Agent 3's (contract
# C6). Invoked as an `exec` module from the `accounting` section of
# sites-enabled/default with input_pairs = request.
#
# Contract C6: POST http://hikrad-acct:8082/acct
#   {record_type:"start|interim|stop", nas_ip, acct_session_id, username,
#    framed_ip, calling_station_id, called_station_id, nas_port_type,
#    nas_port_id, service_type, session_time, bytes_in/out (+gigawords),
#    event_time, terminate_cause?}
# hikrad-acct replies 204 ONLY after durable enqueue (Redis stream + disk
# spill) — that is what makes the pipeline lossless (M2).
#
# LOSSLESS DISCIPLINE: this script fails CLOSED. Any error (backend down,
# timeout, non-2xx) exits non-zero so FreeRADIUS returns `fail` and does NOT
# send an Accounting-Response — the NAS then retransmits, and no packet is
# lost. Only a 2xx (durable enqueue confirmed) lets FreeRADIUS ack the NAS.
use strict;
use warnings;
use HTTP::Tiny;
use JSON::PP qw(encode_json);

# See scripts/authorize.pl's own comment: rlm_exec's input_pairs mode gives
# the child a FRESH environment (mapped request attributes only), so a
# systemd Environment= line for this is silently never seen — the default
# below, native loopback, is what actually applies on every real install.
my $API_URL = $ENV{HIKRAD_ACCT_URL} || 'http://127.0.0.1:8082/acct';

sub unwrap {
    my $v = shift;
    return '' unless defined $v;
    $v =~ s/^"(.*)"$/$1/s;
    $v =~ s/^0[xX]//;
    return $v;
}

# envattr: see scripts/authorize.pl's own comment — rlm_exec's input_pairs
# env-var naming differs by FreeRADIUS build (attribute's literal hyphenated
# name vs. the older upper-cased-with-underscores convention this script was
# first written against); check both so this works regardless of which one
# the installed FreeRADIUS actually uses.
sub envattr {
    my ($hyphen, $underscore) = @_;
    return $ENV{$hyphen} if defined $ENV{$hyphen};
    return $ENV{$underscore} if defined $ENV{$underscore};
    return undef;
}

sub num {
    my $v = unwrap(shift);
    return ($v =~ /^\d+$/) ? $v + 0 : 0;
}

# Coarse pppoe|hotspot hint from Service-Type, or '' when the NAS did not send
# one. Returning '' rather than guessing is deliberate — see the call site.
sub service_hint {
    my $v = unwrap(shift);
    return '' if $v eq '';
    return ($v =~ /login/i) ? 'hotspot' : 'pppoe';
}

# Acct-Status-Type -> C6 record_type. 1=Start, 2=Stop, 3=Interim-Update.
my %status_map = (
    'Start'          => 'start',
    'Stop'           => 'stop',
    'Interim-Update' => 'interim',
    '1'              => 'start',
    '2'              => 'stop',
    '3'              => 'interim',
);
my $status = unwrap(envattr('Acct-Status-Type', 'ACCT_STATUS_TYPE'));
my $record_type = $status_map{$status};
# Accounting-On/Off and any other status types are not session records; ack
# them so the NAS doesn't retransmit, but forward nothing.
exit 0 unless defined $record_type;

my $body = eval {
    encode_json({
        record_type        => $record_type,
        nas_ip             => unwrap(envattr('NAS-IP-Address', 'NAS_IP_ADDRESS')) || unwrap(envattr('NAS-IPv6-Address', 'NAS_IPV6_ADDRESS')),
        acct_session_id    => unwrap(envattr('Acct-Session-Id', 'ACCT_SESSION_ID')),
        username           => unwrap(envattr('User-Name', 'USER_NAME')),
        framed_ip          => unwrap(envattr('Framed-IP-Address', 'FRAMED_IP_ADDRESS')),
        calling_station_id => unwrap(envattr('Calling-Station-Id', 'CALLING_STATION_ID')),
        # Service-instance identification (FR-62 / C7), forwarded RAW and
        # uninterpreted — same contract as the authorize bridge. These let the
        # pipeline file a session under the service instance it actually ran on
        # instead of guessing from the NAS, which is required now that one NAS
        # runs both PPPoE and several hotspot servers.
        called_station_id  => unwrap(envattr('Called-Station-Id', 'CALLED_STATION_ID')),
        nas_port_type      => unwrap(envattr('NAS-Port-Type', 'NAS_PORT_TYPE')),
        nas_port_id        => unwrap(envattr('NAS-Port-Id', 'NAS_PORT_ID')),
        # Coarse kind, and ONLY when the NAS actually told us. A MikroTik sends
        # Service-Type=Login-User on a hotspot Access-Request but omits it from
        # Accounting-Requests, so defaulting a missing value to "pppoe" was a
        # confident lie that filed every hotspot session as pppoe. Empty means
        # "unknown" and the backend identifies the instance from the attributes
        # below instead (docs/ops/known-issues.md, 2026-07-16).
        service_type       => service_hint(envattr('Service-Type', 'SERVICE_TYPE')),
        session_time       => num(envattr('Acct-Session-Time', 'ACCT_SESSION_TIME')),
        bytes_in           => num(envattr('Acct-Input-Octets', 'ACCT_INPUT_OCTETS')),
        bytes_out          => num(envattr('Acct-Output-Octets', 'ACCT_OUTPUT_OCTETS')),
        gigawords_in       => num(envattr('Acct-Input-Gigawords', 'ACCT_INPUT_GIGAWORDS')),
        gigawords_out      => num(envattr('Acct-Output-Gigawords', 'ACCT_OUTPUT_GIGAWORDS')),
        event_time         => unwrap(envattr('Event-Timestamp', 'EVENT_TIMESTAMP')),
        terminate_cause    => unwrap(envattr('Acct-Terminate-Cause', 'ACCT_TERMINATE_CAUSE')),
    });
};
exit 1 if $@ || !defined $body;

my $http = HTTP::Tiny->new(timeout => 3);
my $res = $http->post($API_URL, {
    headers => { 'Content-Type' => 'application/json' },
    content => $body,
});
# Fail closed: only a durable-enqueue 2xx lets us ack the NAS.
exit 1 unless $res->{success};
exit 0;
