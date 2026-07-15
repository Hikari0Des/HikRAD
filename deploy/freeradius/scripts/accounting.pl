#!/usr/bin/perl
# HikRAD FreeRADIUS -> hikrad-acct accounting forward (Phase 2, Agent 2 / RADIUS
# & NAS). Owned config; the ingest endpoint's semantics are Agent 3's (contract
# C6). Invoked as an `exec` module from the `accounting` section of
# sites-enabled/default with input_pairs = request.
#
# Contract C6: POST http://hikrad-acct:8082/acct
#   {record_type:"start|interim|stop", nas_ip, acct_session_id, username,
#    framed_ip, calling_station_id, session_time, bytes_in/out (+gigawords),
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

my $API_URL = $ENV{HIKRAD_ACCT_URL} || 'http://hikrad-acct:8082/acct';

sub unwrap {
    my $v = shift;
    return '' unless defined $v;
    $v =~ s/^"(.*)"$/$1/s;
    $v =~ s/^0[xX]//;
    return $v;
}

sub num {
    my $v = unwrap(shift);
    return ($v =~ /^\d+$/) ? $v + 0 : 0;
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
my $status = unwrap($ENV{ACCT_STATUS_TYPE});
my $record_type = $status_map{$status};
# Accounting-On/Off and any other status types are not session records; ack
# them so the NAS doesn't retransmit, but forward nothing.
exit 0 unless defined $record_type;

my $body = eval {
    encode_json({
        record_type        => $record_type,
        nas_ip             => unwrap($ENV{NAS_IP_ADDRESS}) || unwrap($ENV{NAS_IPV6_ADDRESS}),
        acct_session_id    => unwrap($ENV{ACCT_SESSION_ID}),
        username           => unwrap($ENV{USER_NAME}),
        framed_ip          => unwrap($ENV{FRAMED_IP_ADDRESS}),
        calling_station_id => unwrap($ENV{CALLING_STATION_ID}),
        session_time       => num($ENV{ACCT_SESSION_TIME}),
        bytes_in           => num($ENV{ACCT_INPUT_OCTETS}),
        bytes_out          => num($ENV{ACCT_OUTPUT_OCTETS}),
        gigawords_in       => num($ENV{ACCT_INPUT_GIGAWORDS}),
        gigawords_out      => num($ENV{ACCT_OUTPUT_GIGAWORDS}),
        event_time         => unwrap($ENV{EVENT_TIMESTAMP}),
        terminate_cause    => unwrap($ENV{ACCT_TERMINATE_CAUSE}),
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
