package main

// `license-tool activate` — the FR-123 file-exchange activation flow. The
// buyer's server exports an activation-request file carrying its OWN
// current fingerprint (never a value anyone typed); this command reads that
// file, checks its self-checksum, and signs a license against exactly the
// fingerprint it carries. That is the whole fix: a stale or mistyped
// fingerprint can no longer make its way into a signed license, because the
// only fingerprint this command ever sees is the one the request file says.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// activationRequest mirrors internal/platform/license.ActivationRequest
// exactly (same reason every other type in this file mirrors its backend
// counterpart: separate Go module, wire format is the contract).
type activationRequest struct {
	SchemaVersion  int       `json:"schema_version"`
	MachineIDHash  string    `json:"machine_id_hash"`
	MACHash        string    `json:"mac_hash"`
	Fingerprint    string    `json:"fingerprint"`
	CurrentKeyID   string    `json:"current_key_id,omitempty"`
	CurrentSerial  string    `json:"current_serial,omitempty"`
	ProductVersion string    `json:"product_version"`
	GeneratedAt    time.Time `json:"generated_at"`
	Checksum       string    `json:"checksum"`
}

// verifyChecksum mirrors license.VerifyActivationRequestChecksum's exact
// algorithm: sha256 over the JSON encoding of req with Checksum zeroed.
func verifyChecksum(req activationRequest) bool {
	if req.Checksum == "" {
		return false
	}
	want := req.Checksum
	req.Checksum = ""
	raw, err := json.Marshal(req)
	if err != nil {
		return false
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]) == want
}

func cmdActivate(args []string) error {
	fs := flag.NewFlagSet("activate", flag.ExitOnError)
	requestFile := fs.String("request", "", "path to the activation-request file exported by the buyer's server (required)")
	keyFile := fs.String("key", "", "path to the base64 private key file from keygen")
	keyID := fs.String("keyid", "", "vendor-assigned key id, e.g. K-2026-0001")
	licensee := fs.String("licensee", "", "licensee/ISP name")
	tier := fs.String("tier", "", "subscriber tier label, e.g. pro")
	maxSubs := fs.Int("max-subscribers", -1, "max-subscriber entitlement (>=0; 0 = unlimited)")
	version := fs.String("version", "1", "entitled major version")
	entitlements := fs.String("entitlements", "", "comma-separated entitlement names")
	expires := fs.String("expires", "", "RFC3339 expiry; blank = perpetual")
	supportUntil := fs.String("support-until", "", "RFC3339 support-window end")
	serial := fs.String("serial", "", "vendor-assigned serial/order number")
	out := fs.String("out", "", "output file (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *requestFile == "" {
		return fmt.Errorf("-request is required (the activation-request file exported by the buyer's server)")
	}

	req, err := loadActivationRequest(*requestFile)
	if err != nil {
		return err
	}
	if !verifyChecksum(req) {
		return fmt.Errorf("activate: %s failed its checksum check — hand-edited or corrupted, refusing to sign against it", *requestFile)
	}
	fmt.Fprintf(os.Stderr, "Activation request OK: fingerprint %s (product %s, generated %s)\n",
		req.Fingerprint, req.ProductVersion, req.GeneratedAt.Format(time.RFC3339))
	if req.CurrentKeyID != "" {
		fmt.Fprintf(os.Stderr, "  currently installed key: %s (serial %s)\n", req.CurrentKeyID, req.CurrentSerial)
	}

	// Flag-driven form: every field beyond -request supplied. Interactive
	// form: nothing but -request given on a terminal.
	haveAnyFlag := *keyFile != "" || *keyID != "" || *licensee != "" || *tier != "" || *maxSubs >= 0 ||
		*entitlements != "" || *expires != "" || *supportUntil != "" || *serial != ""
	var in issueInput
	if !haveAnyFlag {
		if !stdinIsTerminal() {
			return fmt.Errorf("activate: no signing flags given and stdin is not a terminal — pass -key/-keyid/-licensee/-tier/-max-subscribers (see -h) or run interactively")
		}
		in, err = promptActivate(req)
		if err != nil {
			return err
		}
	} else {
		for name, v := range map[string]string{"key": *keyFile, "keyid": *keyID, "licensee": *licensee, "tier": *tier} {
			if v == "" {
				return fmt.Errorf("-%s is required", name)
			}
		}
		if *maxSubs < 0 {
			return fmt.Errorf("-max-subscribers is required and must be >= 0 (0 = unlimited)")
		}
		expiresAt, err := parseOptionalTime(*expires)
		if err != nil {
			return fmt.Errorf("-expires: %w", err)
		}
		supportUntilAt, err := parseOptionalTime(*supportUntil)
		if err != nil {
			return fmt.Errorf("-support-until: %w", err)
		}
		in = issueInput{
			keyFile: *keyFile, keyID: *keyID, licensee: *licensee, tier: *tier,
			maxSubs: *maxSubs, version: *version, out: *out,
			entitlements: splitEntitlements(*entitlements),
			expiresAt:    expiresAt,
			supportUntil: supportUntilAt,
			serial:       *serial,
		}
		if in.serial == "" {
			in.serial = req.CurrentSerial
		}
	}

	// The one line that matters: the fingerprint signed into the license is
	// ALWAYS req.Fingerprint, straight from the file, never anything the
	// operator typed or edited on the way in.
	in.fingerprint = req.Fingerprint
	in.issuedAt = time.Now().UTC()
	return signAndWrite(in)
}

func loadActivationRequest(path string) (activationRequest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return activationRequest{}, fmt.Errorf("read activation request: %w", err)
	}
	var req activationRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return activationRequest{}, fmt.Errorf("parse activation request: %w", err)
	}
	if req.Fingerprint == "" {
		return activationRequest{}, fmt.Errorf("activation request has no fingerprint")
	}
	return req, nil
}

// promptActivate walks the vendor through the remaining license fields once
// the activation request's fingerprint is already known and verified — the
// fingerprint itself is never asked for.
func promptActivate(req activationRequest) (issueInput, error) {
	r := bufio.NewReader(os.Stdin)
	keyFile, err := promptRequired(r, "Private key file (from `license-tool keygen -out`)")
	if err != nil {
		return issueInput{}, err
	}
	keyID, err := promptRequired(r, "Key id (e.g. K-2026-0001)")
	if err != nil {
		return issueInput{}, err
	}
	licensee, err := promptRequired(r, "Licensee / ISP name")
	if err != nil {
		return issueInput{}, err
	}
	tier, maxSubs, defaultEnts, err := promptTier(r)
	if err != nil {
		return issueInput{}, err
	}
	version, err := promptWithDefault(r, "Entitled major version", "1")
	if err != nil {
		return issueInput{}, err
	}
	entitlements, err := promptEntitlements(r, defaultEnts)
	if err != nil {
		return issueInput{}, err
	}
	expiresStr, err := promptWithDefault(r, "Expires (RFC3339, blank = perpetual)", "")
	if err != nil {
		return issueInput{}, err
	}
	expiresAt, err := parseOptionalTime(expiresStr)
	if err != nil {
		return issueInput{}, fmt.Errorf("expires: %w", err)
	}
	supportStr, err := promptWithDefault(r, "Support until (RFC3339, blank = unset)", "")
	if err != nil {
		return issueInput{}, err
	}
	supportUntil, err := parseOptionalTime(supportStr)
	if err != nil {
		return issueInput{}, fmt.Errorf("support-until: %w", err)
	}
	serial, err := promptWithDefault(r, "Serial / order number", req.CurrentSerial)
	if err != nil {
		return issueInput{}, err
	}
	out, err := promptWithDefault(r, "Output file (blank = stdout)", "")
	if err != nil {
		return issueInput{}, err
	}
	return issueInput{
		keyFile: keyFile, keyID: keyID, licensee: licensee, tier: tier,
		maxSubs: maxSubs, version: version, out: out,
		entitlements: entitlements,
		expiresAt:    expiresAt,
		supportUntil: supportUntil,
		serial:       strings.TrimSpace(serial),
	}, nil
}
