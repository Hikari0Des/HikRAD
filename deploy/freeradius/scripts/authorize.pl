#!/usr/bin/perl
# HikRAD FreeRADIUS -> backend authorize bridge (Phase 1, Agent 2 / RADIUS &
# NAS). Invoked as an `exec` module from the `authorize` section of
# sites-enabled/default with input_pairs = request: FreeRADIUS puts every
# request attribute into an environment variable (name upper-cased, '-' ->
# '_'; string values arrive double-quoted, octets arrive "0x"-prefixed hex).
#
# rlm_rest is not used for the HTTP call: its connect_uri reachability
# pre-check runs at server *startup* (before hikrad-api may be healthy) and
# aborts the whole virtual server if it fails, and this build's rlm_json has
# no response-decode/map support to turn our C4 JSON body into attributes.
# A single script making the call and printing the decision keeps both
# concerns (transport + mapping) in one place and fails safe on error.
#
# Contract C4: POST /internal/radius/authorize, request
# {username,password?,chap_challenge?,chap_response?,nas_ip,
#  calling_station_id?,service,called_station_id?,nas_port_type?,nas_port_id?},
# response
# {action,reason,attributes:[{intent,value}]}.
#
# stdout is parsed by rlm_exec as `Attribute := value` pairs (no per-line
# list prefix — rlm_exec's pairlist parser rejects "list:Attribute" syntax;
# confirmed empirically against 3.2.3). All of this script's output lands in
# the reply list (output_pairs = reply, mods-available/hikrad_authorize):
# Tmp-String-0/-1 are FreeRADIUS's generic scratch attributes (never encoded
# on the wire) that carry the accept/reject decision for the unlang driver in
# sites-enabled/default to branch on; on accept, the vendor-neutral intents
# become the real reply attributes FreeRADIUS sends to the NAS
# (Mikrotik-Rate-Limit / Framed-Pool / Session-Timeout).
use strict;
use warnings;
use HTTP::Tiny;
use JSON::PP qw(encode_json decode_json);

my $API_URL = $ENV{HIKRAD_AUTHORIZE_URL} || 'http://hikrad-api:8080/internal/radius/authorize';

sub unwrap {
    my $v = shift;
    return '' unless defined $v;
    $v =~ s/^"(.*)"$/$1/s;
    $v =~ s/^0[xX]//;
    return $v;
}

my $username = unwrap($ENV{USER_NAME});
my $password = unwrap($ENV{USER_PASSWORD});
my $chap_challenge = unwrap($ENV{CHAP_CHALLENGE});
my $chap_response  = unwrap($ENV{CHAP_PASSWORD});
my $nas_ip   = unwrap($ENV{NAS_IP_ADDRESS}) || unwrap($ENV{NAS_IPV6_ADDRESS});
my $calling  = unwrap($ENV{CALLING_STATION_ID});

# Service-instance identification (FR-62 / contract C7): forwarded RAW and
# uninterpreted. Which of a NAS's hotspot/PPPoE service instances a request
# belongs to is decided by the Go vendor adapter, which is the only place that
# knows how a given vendor encodes it (e.g. MikroTik puts the hotspot server
# name in Called-Station-Id). This script's job here is to forward, not to
# interpret — keep it that way, or instance identity stops being vendor-neutral.
my $called        = unwrap($ENV{CALLED_STATION_ID});
my $nas_port_type = unwrap($ENV{NAS_PORT_TYPE});
my $nas_port_id   = unwrap($ENV{NAS_PORT_ID});

# Service discrimination (FR-58): MikroTik Hotspot logins arrive as
# Service-Type = Login-User, PPPoE as Framed-User. This stays the COARSE hint
# only — the backend supersedes it with the resolved instance's own service.
# Match case-insensitively since rlm_exec may render the value as the enum name
# or a raw number.
my $service_type = unwrap($ENV{SERVICE_TYPE});
my $service = ($service_type =~ /login/i) ? "hotspot" : "pppoe";

sub emit_reject {
    my ($reason) = @_;
    print qq{Tmp-String-0 := "reject"\n};
    print qq{Tmp-String-1 := "$reason"\n};
    exit 0;
}

sub emit_internal_error {
    # Fail closed: NFR-8/agent-2 edge case is "backend-down rejects within
    # 2s, not a hang" — this script's own HTTP timeout enforces the 2s, and
    # any failure (timeout, malformed JSON, non-2xx) becomes a reject.
    emit_reject("unknown_nas");
}

my $body = eval {
    encode_json({
        username            => $username,
        password            => $password,
        chap_challenge      => $chap_challenge,
        chap_response       => $chap_response,
        nas_ip              => $nas_ip,
        calling_station_id  => $calling,
        service             => $service,
        called_station_id   => $called,
        nas_port_type       => $nas_port_type,
        nas_port_id         => $nas_port_id,
    });
};
emit_internal_error() if $@ || !defined $body;

my $http = HTTP::Tiny->new(timeout => 2);
my $res = $http->post($API_URL, {
    headers => { 'Content-Type' => 'application/json' },
    content => $body,
});
emit_internal_error() unless $res->{success};

my $decoded = eval { decode_json($res->{content}) };
emit_internal_error() if $@ || !defined $decoded;

if (($decoded->{action} // '') ne 'accept') {
    emit_reject($decoded->{reason} // 'bad_password');
}

print qq{Tmp-String-0 := "accept"\n};
for my $attr (@{ $decoded->{attributes} // [] }) {
    my $intent = $attr->{intent} // '';
    my $value  = $attr->{value}  // '';
    $value =~ s/(["\\])/\\$1/g;
    if ($intent eq 'rate_limit') {
        print qq{Mikrotik-Rate-Limit := "$value"\n};
    } elsif ($intent eq 'address_pool') {
        print qq{Framed-Pool := "$value"\n};
    } elsif ($intent eq 'static_ip') {
        # Framed-IP-Address takes precedence over Framed-Pool (FR-16.2); the
        # backend never emits both for one accept.
        print qq{Framed-IP-Address := $value\n};
    } elsif ($intent eq 'session_timeout') {
        print qq{Session-Timeout := $value\n};
    } elsif ($intent eq 'redirect_expired') {
        # Walled-garden address-list the router's expired-redirect rules match.
        print qq{Mikrotik-Address-List := "$value"\n};
    }
}
exit 0;
