#!/usr/bin/perl
# HikRAD FreeRADIUS -> backend dynamic-client bridge (v4-2, FR-118). Invoked
# as an `exec` module (mods-available/hikrad_dynamic_client) from
# sites-available/dynamic-clients' authorize {} section, with
# input_pairs = request: FreeRADIUS puts every request attribute into an
# environment variable, same convention as scripts/authorize.pl.
#
# This is the mechanism that lets a NAS added through the panel authenticate
# with ZERO FreeRADIUS restart (gate item 9): clients.conf's broad
# `hikrad_dynamic` range triggers this lookup the first time a packet
# arrives from a source IP FreeRADIUS doesn't already have a client for.
# FreeRADIUS's control-socket "reload" does not re-read clients.conf
# (documented, confirmed live against 3.2.3); only this per-packet,
# in-memory-cached discovery mechanism is actually restart-free.
#
# Contract: POST /internal/radius/dynamic-client, request {nas_ip}, response
# {found, ip?, secret?, shortname?, require_message_authenticator?}.
#
# stdout is parsed by rlm_exec as `Attribute := value` pairs, landing in the
# CONTROL list (output_pairs = control, mods-available/hikrad_dynamic_client)
# — the FreeRADIUS-Client-* attributes that its own built-in `dynamic_clients`
# policy call reads to actually create the in-memory client. Printing nothing
# and exiting non-zero (a miss, or any failure) makes `ok` false in the
# calling unlang, which rejects instead of creating a client — fail closed,
# same posture as authorize.pl.
use strict;
use warnings;
use HTTP::Tiny;
use JSON::PP qw(encode_json decode_json);

# Native loopback default, not settable via the systemd unit's own
# Environment= — see scripts/authorize.pl's comment: rlm_exec's
# input_pairs mode gives every exec'd child a FRESH environment (mapped
# request attributes only), never the parent radiusd process's own.
my $API_URL = $ENV{HIKRAD_DYNAMIC_CLIENT_URL} || 'http://127.0.0.1:8080/internal/radius/dynamic-client';

sub unwrap {
    my $v = shift;
    return '' unless defined $v;
    $v =~ s/^"(.*)"$/$1/s;
    $v =~ s/^0[xX]//;
    return $v;
}

# envattr: see scripts/authorize.pl's own comment — rlm_exec's input_pairs
# env-var naming differs by FreeRADIUS build; check both forms.
sub envattr {
    my ($hyphen, $underscore) = @_;
    return $ENV{$hyphen} if defined $ENV{$hyphen};
    return $ENV{$underscore} if defined $ENV{$underscore};
    return undef;
}

# Packet-Src-IP-Address is the actual socket source address (what a real NAS
# connected from); NAS-IP-Address is an attacker/client-controlled attribute
# value and must never be trusted for this lookup.
my $src_ip = unwrap(envattr('Packet-Src-IP-Address', 'PACKET_SRC_IP_ADDRESS')) || unwrap(envattr('Packet-Src-IPv6-Address', 'PACKET_SRC_IPV6_ADDRESS'));

exit 1 unless $src_ip;

my $body = eval { encode_json({ nas_ip => $src_ip }) };
exit 1 if $@ || !defined $body;

my $http = HTTP::Tiny->new(timeout => 2);
my $res = $http->post($API_URL, {
    headers => { 'Content-Type' => 'application/json' },
    content => $body,
});
exit 1 unless $res->{success};

my $decoded = eval { decode_json($res->{content}) };
exit 1 if $@ || !defined $decoded;
exit 1 unless $decoded->{found};

my $secret = $decoded->{secret} // '';
$secret =~ s/(["\\])/\\$1/g;
my $shortname = $decoded->{shortname} // $src_ip;
$shortname =~ s/(["\\])/\\$1/g;

print qq{FreeRADIUS-Client-IP-Address := "$src_ip"\n};
print qq{FreeRADIUS-Client-Secret := "$secret"\n};
print qq{FreeRADIUS-Client-Shortname := "$shortname"\n};
print qq{FreeRADIUS-Client-NAS-Type := "other"\n};
print qq{FreeRADIUS-Client-Require-MA := no\n};
exit 0;
