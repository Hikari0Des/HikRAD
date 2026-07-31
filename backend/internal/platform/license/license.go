// Package license is the offline Ed25519 license system (Phase 5, Agent 1;
// contract C4, FR-50). It has no database dependency — internal/platform
// wires it to the `license` table (migration 0410) and the HTTP surface; this
// package only knows how to verify a signed blob, compute/compare server
// fingerprints, and run the grace state machine. Kept dependency-free so it
// can be unit-tested without Postgres and reused by scripts/license-tool.
package license

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"time"
)

// ProductionPublicKey is HikRAD's vendor Ed25519 public key, embedded in the
// binary per FR-50.1. The matching private key never ships in a product
// image: scripts/license-tool holds it (kept out of shipped images by the
// Dockerfiles, which COPY only backend/ and frontend/ build output).
//
// Regenerating the keypair (e.g. for a white-label reseller build) means
// recompiling this constant from the new public key — that coupling is
// deliberate: it is what makes a leaked/cracked key unable to sign license
// blobs for binaries built from this source.
const ProductionPublicKeyB64 = "TrjUPG6wqv/lX45zs5bGRWo8F1zmS3p+pdnSlFOzL7g="

// ProductionPublicKey is the decoded form of ProductionPublicKeyB64, ready to
// pass to Verify. It panics on package init if the embedded constant is ever
// hand-edited into something that doesn't decode — a build-time failure is
// far preferable to a silently-broken license check in the field.
var ProductionPublicKey = mustDecodePublicKey(ProductionPublicKeyB64)

func mustDecodePublicKey(b64 string) ed25519.PublicKey {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		panic("license: embedded public key does not decode: " + err.Error())
	}
	if len(raw) != ed25519.PublicKeySize {
		panic(fmt.Sprintf("license: embedded public key is %d bytes, want %d", len(raw), ed25519.PublicKeySize))
	}
	return ed25519.PublicKey(raw)
}

// ErrInvalidSignature is returned by Verify when the signature does not
// authenticate under the given public key (tampered payload, wrong key, or a
// corrupt blob).
var ErrInvalidSignature = errors.New("license: invalid signature")

// ErrMalformedPayload is returned when the payload JSON is structurally
// invalid or missing a required field.
var ErrMalformedPayload = errors.New("license: malformed payload")

// Payload is the signed license content (FR-50.1): licensee name, issue date,
// max-subscriber tier, and entitled major version, plus the fingerprint the
// key was issued for and a vendor-assigned key id.
type Payload struct {
	KeyID           string    `json:"key_id"`
	Licensee        string    `json:"licensee"`
	IssuedAt        time.Time `json:"issued_at"`
	Tier            string    `json:"tier"`
	MaxSubscribers  int       `json:"max_subscribers"`
	EntitledVersion string    `json:"entitled_version"`
	Fingerprint     string    `json:"fingerprint"`
}

// Blob is the wire format uploaded via POST /api/v1/license: the payload
// exactly as signed (so re-marshaling can't change the bytes under the
// signature) plus a base64 Ed25519 signature over those bytes.
type Blob struct {
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature"`
}

// Sign is used by scripts/license-tool (the vendor keygen/issuer) to produce
// a Blob from a Payload and the vendor private key. It lives here (not a
// separate tool-only package) so the tool and the verifier share one exact
// encoding — a mismatch between "how we sign" and "how we verify" is the
// classic way to ship a license system that silently rejects everything.
func Sign(priv ed25519.PrivateKey, p Payload) (Blob, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return Blob{}, fmt.Errorf("%w: %v", ErrMalformedPayload, err)
	}
	sig := ed25519.Sign(priv, raw)
	return Blob{
		Payload:   raw,
		Signature: base64.StdEncoding.EncodeToString(sig),
	}, nil
}

// Verify authenticates blob under pub and decodes its payload. It never
// trusts anything about the payload until the signature check passes.
func Verify(pub ed25519.PublicKey, blob Blob) (Payload, error) {
	sig, err := base64.StdEncoding.DecodeString(blob.Signature)
	if err != nil {
		return Payload{}, fmt.Errorf("%w: signature is not valid base64", ErrInvalidSignature)
	}
	if len(blob.Payload) == 0 {
		return Payload{}, fmt.Errorf("%w: empty payload", ErrMalformedPayload)
	}
	if !ed25519.Verify(pub, blob.Payload, sig) {
		return Payload{}, ErrInvalidSignature
	}
	var p Payload
	if err := json.Unmarshal(blob.Payload, &p); err != nil {
		return Payload{}, fmt.Errorf("%w: %v", ErrMalformedPayload, err)
	}
	if p.KeyID == "" || p.Licensee == "" || p.Fingerprint == "" {
		return Payload{}, fmt.Errorf("%w: missing key_id/licensee/fingerprint", ErrMalformedPayload)
	}
	return p, nil
}

// --- Fingerprint (FR-50.2) ---------------------------------------------------

// componentSep joins the two hashed fingerprint components in the string
// form stored/displayed everywhere (license.fingerprint column, the wizard's
// copyable text, the payload's fingerprint field).
const componentSep = ":"

// ErrNoFingerprint is returned when neither a machine-id nor a MAC address
// could be read — an environment too stripped-down to identify at all.
var ErrNoFingerprint = errors.New("license: could not determine machine-id or MAC address")

// Current computes this server's fingerprint from /etc/machine-id (or
// /var/lib/dbus/machine-id, or HIKRAD_MACHINE_ID_OVERRIDE for environments
// with neither) plus the lowest-named non-loopback interface's MAC. Compose
// mounts /etc/machine-id read-only from the host into hikrad-api specifically
// so this identifies the host, not the container (a container recreated by
// `docker compose up` must not look like new hardware).
func Current() (string, error) {
	mid, midErr := machineID()
	mac, macErr := primaryMAC()
	if midErr != nil && macErr != nil {
		return "", ErrNoFingerprint
	}
	return Compose(mid, mac), nil
}

// Compose builds the fingerprint string from its two raw components (machine
// id, primary MAC). Exported so tests and scripts/license-tool can construct
// fingerprints without touching the filesystem/network stack.
func Compose(machineID, mac string) string {
	return hashComponent(machineID) + componentSep + hashComponent(mac)
}

func hashComponent(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}

func machineID() (string, error) {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(p); err == nil {
			if s := strings.TrimSpace(string(b)); s != "" {
				return s, nil
			}
		}
	}
	if v := os.Getenv("HIKRAD_MACHINE_ID_OVERRIDE"); v != "" {
		return v, nil
	}
	return "", errors.New("license: no machine-id source available")
}

// virtualIfacePrefixes name the software/tunnel interfaces the kernel exposes
// with synthetic or all-zero MACs. They must never be chosen as the
// fingerprint's "primary MAC": which of them exist is transient state (a VPN
// raises gre0/erspan0/ip6tnl0, Docker raises veth*/docker0), and several sort
// alphabetically *before* a real eth0/ens* NIC — so including them made the MAC
// half of the fingerprint flap between the real NIC and a tunnel's
// 00:00:00:00:00:00 every time the tunnel set changed, spuriously re-entering
// grace mode on each flip (the exact symptom seen with an admin on VPN). See
// docs/ops/known-issues.md 2026-07-24.
var virtualIfacePrefixes = []string{
	"erspan", "gre", "gretap", "ip6gre", "ip6tnl", "ip6_vti", "ip_vti", "vti",
	"sit", "tunl", "tun", "tap", "wg", "vxlan", "bond", "dummy",
	"docker", "veth", "br-", "virbr", "vmnet", "nlmon",
}

func macAllZero(mac net.HardwareAddr) bool {
	for _, b := range mac {
		if b != 0 {
			return false
		}
	}
	return true
}

// isVirtualIface reports whether an interface is a software/tunnel device that
// must not anchor the fingerprint — an all-zero MAC or a known virtual name.
func isVirtualIface(name string, mac net.HardwareAddr) bool {
	if macAllZero(mac) {
		return true
	}
	for _, p := range virtualIfacePrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func primaryMAC() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	var physical, fallback []net.Interface
	for _, i := range ifaces {
		if i.Flags&net.FlagLoopback != 0 || len(i.HardwareAddr) == 0 {
			continue
		}
		fallback = append(fallback, i)
		if !isVirtualIface(i.Name, i.HardwareAddr) {
			physical = append(physical, i)
		}
	}
	// Prefer real NICs; only if none exist (a stripped container that has just
	// a tunnel) fall back to any MAC-bearing interface rather than erroring —
	// the host machine-id remains the fingerprint's stable anchor either way.
	pick := physical
	if len(pick) == 0 {
		pick = fallback
	}
	if len(pick) == 0 {
		return "", errors.New("license: no non-loopback interface with a MAC address")
	}
	// Deterministic choice: lowest interface name (eth0 before eth1, etc.) so
	// the "primary" interface doesn't depend on kernel enumeration order.
	sort.Slice(pick, func(a, b int) bool { return pick[a].Name < pick[b].Name })
	return pick[0].HardwareAddr.String(), nil
}

// Components splits a fingerprint string into its two hashed halves
// (machine-id hash, MAC hash). ok is false for a malformed fingerprint that
// doesn't contain the separator. Used by the licensing UI (FR-104) to tell the
// operator *which* component drifted when a re-issue is needed.
func Components(fp string) (machineIDHash, macHash string, ok bool) {
	parts := strings.SplitN(fp, componentSep, 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// CompareComponents reports which of the two fingerprint components match
// between the stored (issued) and current fingerprints. A malformed input on
// either side yields both-false. This is the per-component view behind
// WithinTolerance (which tolerates up to one differing component), surfaced so
// the License page can say "network card changed" vs "both changed — re-issue".
func CompareComponents(stored, current string) (machineMatch, macMatch bool) {
	sMid, sMac, sOK := Components(stored)
	cMid, cMac, cOK := Components(current)
	if !sOK || !cOK {
		return false, false
	}
	return sMid == cMid, sMac == cMac
}

// WithinTolerance reports whether stored and current fingerprints are close
// enough to avoid grace mode: an exact match, or exactly one of the two
// hashed components differs. That tolerates the common single-component
// drift of a VM clone (hypervisor reassigns the virtual MAC, machine-id
// stays because it's on the cloned disk) or a disk migration to new hardware
// (machine-id travels with the OS image, MAC changes) without punishing a
// legitimate admin for routine virtualization events. Both components
// differing means the install genuinely moved to different hardware and
// grace mode is the correct, generous response (never an outright block).
func WithinTolerance(stored, current string) bool {
	if stored == current {
		return true
	}
	sp := strings.SplitN(stored, componentSep, 2)
	cp := strings.SplitN(current, componentSep, 2)
	if len(sp) != 2 || len(cp) != 2 {
		return false
	}
	diffs := 0
	if sp[0] != cp[0] {
		diffs++
	}
	if sp[1] != cp[1] {
		diffs++
	}
	return diffs <= 1
}
