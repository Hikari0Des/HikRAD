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


# HIKRAD_AUTHORIZE_URL is NOT actually settable via the systemd unit's own
# Environment= (found live, the real root cause behind the packet-harness
# gate hanging for ~10s per request then failing): rlm_exec's input_pairs
# mode builds a FRESH environment for the child containing only the mapped
# request attributes — it does not inherit the parent radiusd process's own
# environment at all, so any systemd `Environment=` line here is silently
# never seen. The default below is therefore the value that actually
# matters on every real install; it is native loopback, not the old Docker
# service hostname ("hikrad-api", unresolvable natively — the ~10s hang was
# DNS resolution failing before HTTP::Tiny's own 2s socket timeout ever got
# a chance to apply, since that timeout does not bound the resolver call).
# $ENV{HIKRAD_AUTHORIZE_URL} is kept as an override for anyone invoking this
# script directly (outside rlm_exec) where a real environment does exist.
my $API_URL = $ENV{HIKRAD_AUTHORIZE_URL} || 'http://127.0.0.1:8080/internal/radius/authorize';

sub unwrap {
    my $v = shift;
    return '' unless defined $v;
    $v =~ s/^"(.*)"$/$1/s;
    $v =~ s/^0[xX]//;
    return $v;
}

# envattr(HyphenName, UNDERSCORE_NAME): rlm_exec's input_pairs env-var
# naming is NOT what every FreeRADIUS 3.2.x build actually does — this
# module's own original comment ("name upper-cased, '-' -> '_'") was
# confirmed against 3.2.3, but 3.2.7 (Debian trixie's freeradius package,
# found live installing v4-2's native installer for real) sets the
# environment variable to the attribute's OWN literal name instead
# ("User-Name", not "USER_NAME") — Perl's %ENV has no restriction on hyphens
# in a key, so this went completely undetected by anything short of a real
# end-to-end run. Check both forms so this script works across FreeRADIUS
# builds without needing to know which convention the one installed here uses.
sub envattr {
    my ($hyphen, $underscore) = @_;
    return $ENV{$hyphen} if defined $ENV{$hyphen};
    return $ENV{$underscore} if defined $ENV{$underscore};
    return undef;
}

my $username = unwrap(envattr('User-Name', 'USER_NAME'));
my $password = unwrap(envattr('User-Password', 'USER_PASSWORD'));
my $chap_challenge = unwrap(envattr('CHAP-Challenge', 'CHAP_CHALLENGE'));
my $chap_response  = unwrap(envattr('CHAP-Password', 'CHAP_PASSWORD'));
my $nas_ip   = unwrap(envattr('NAS-IP-Address', 'NAS_IP_ADDRESS')) || unwrap(envattr('NAS-IPv6-Address', 'NAS_IPV6_ADDRESS'));
my $calling  = unwrap(envattr('Calling-Station-Id', 'CALLING_STATION_ID'));

# Service-instance identification (FR-62 / contract C7): forwarded RAW and
# uninterpreted. Which of a NAS's hotspot/PPPoE service instances a request
# belongs to is decided by the Go vendor adapter, which is the only place that
# knows how a given vendor encodes it (e.g. MikroTik puts the hotspot server
# name in Called-Station-Id). This script's job here is to forward, not to
# interpret — keep it that way, or instance identity stops being vendor-neutral.
my $called        = unwrap(envattr('Called-Station-Id', 'CALLED_STATION_ID'));
my $nas_port_type = unwrap(envattr('NAS-Port-Type', 'NAS_PORT_TYPE'));
my $nas_port_id   = unwrap(envattr('NAS-Port-Id', 'NAS_PORT_ID'));

# Service discrimination (FR-58): MikroTik Hotspot logins arrive as
# Service-Type = Login-User, PPPoE as Framed-User. This stays the COARSE hint
# only — the backend supersedes it with the resolved instance's own service.
# Match case-insensitively since rlm_exec may render the value as the enum name
# or a raw number.
my $service_type = unwrap(envattr('Service-Type', 'SERVICE_TYPE'));
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
    } elsif ($intent eq 'address_list') {
        # FR-109: operator-named address-list applied at a NORMAL login. Same
        # VSA as redirect_expired; the backend emits only one per accept
        # (address_list in modeNormal/Throttled, redirect_expired in expired).
        print qq{Mikrotik-Address-List := "$value"\n};
    } elsif ($intent eq 'delegated_prefix') {
        # v4-7 / FR-149: a static per-subscriber IPv6 prefix pin. Standard
        # RFC 3162 attribute, not a Mikrotik VSA (unlike every other intent
        # above) — the one case where the vendor-neutral name IS the wire
        # attribute name.
        print qq{Framed-IPv6-Prefix := $value\n};
    }
}
exit 0;
