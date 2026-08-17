// Command license-tool is HikRAD's vendor-side license issuer (FR-50.1/50.4,
// sub-PRD 01 §2 "License"). It never ships in a product image — it lives
// under scripts/ at the repo root, outside the `backend/` tree the
// Dockerfiles COPY, and is its own Go module so it cannot even be built by
// `cd backend && go build ./...`.
//
// The buyer's server never talks to this tool or anywhere it runs: the
// server prints its fingerprint (GET /api/v1/setup/license or
// /api/v1/license, or `hikrad license fingerprint`), the buyer sends that to
// the vendor by whatever offline channel (email, ticket, USB), the vendor
// runs `license-tool issue` here and sends back the resulting JSON file, and
// the buyer uploads it in the wizard or Settings > System. No network call
// happens on either side as part of this exchange (NFR-7, AC-50a).
//
// The Payload struct and signing scheme below are a deliberate mirror of
// backend/internal/platform/license (Payload fields/JSON tags, plain
// ed25519.Sign over the exact marshaled JSON bytes, std-base64 signature):
// this tool cannot import that package (it is a different Go module, and
// "internal" packages are only importable from within their own module
// tree), so the wire format is the contract between the two instead. Do not
// change either side without changing both.
package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// tierOption is one row of the FR-105 tier catalog the interactive issuer
// offers as a dropdown. The catalog is the vendor-side source of truth for
// tier→max-subscribers; the backend enforces the number, not the label
// (license.Verify never validates the tier string), so this list can grow
// without a product rebuild. Keep it in sync with the C2 table in
// docs/v3/phases/phase-v3-3-licensing-hardening/00-phase.md.
//
// DefaultEntitlements (FR-122) is this catalog's pre-checked entitlement set
// for a new v2 license of that tier — a vendor-side sales default, separate
// from (and not to be confused with) internal/platform/license's
// DefaultEntitlements(tier), which is the runtime fallback grant for
// pre-existing v1 blobs. white_label is deliberately unchecked for every
// stock tier: it is sold as an explicit add-on, never bundled silently.
type tierOption struct {
	Key                 string
	Label               string
	Max                 int // 0 => unlimited (no cap)
	DefaultEntitlements []string
}

var tierCatalog = []tierOption{
	{Key: "starter", Label: "Starter — 200 subscribers", Max: 200, DefaultEntitlements: []string{"self_service"}},
	{Key: "standard", Label: "Standard — 500 subscribers", Max: 500, DefaultEntitlements: []string{"self_service", "api_access"}},
	{Key: "pro", Label: "Pro — 1000 subscribers", Max: 1000, DefaultEntitlements: []string{"self_service", "api_access", "gateways"}},
	{Key: "unlimited", Label: "Unlimited — no cap", Max: 0, DefaultEntitlements: []string{"self_service", "api_access", "gateways"}},
}

// knownEntitlements mirrors internal/platform/license.KnownEntitlements —
// duplicated for the same reason the payload/blob structs are: this module
// cannot import backend's internal package, so the wire contract (here, the
// set of names the interactive prompt/serve form offer) is kept in sync by
// hand, same as everything else in this file's mirrored types.
var knownEntitlements = []string{"white_label", "self_service", "api_access", "gateways"}

// payload mirrors internal/platform/license.Payload exactly, including the
// FR-122 v2 fields (Version/Entitlements/ExpiresAt/SupportUntil/Serial), all
// omitempty so an issuer that never sets them keeps emitting byte-identical
// v1 output — no behavior change for existing non-interactive callers.
type payload struct {
	KeyID           string    `json:"key_id"`
	Licensee        string    `json:"licensee"`
	IssuedAt        time.Time `json:"issued_at"`
	Tier            string    `json:"tier"`
	MaxSubscribers  int       `json:"max_subscribers"`
	EntitledVersion string    `json:"entitled_version"`
	Fingerprint     string    `json:"fingerprint"`

	Version      int        `json:"version,omitempty"`
	Entitlements []string   `json:"entitlements,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	SupportUntil *time.Time `json:"support_until,omitempty"`
	Serial       string     `json:"serial,omitempty"`
}

// blob mirrors internal/platform/license.Blob exactly.
type blob struct {
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = cmdKeygen(os.Args[2:])
	case "issue":
		err = cmdIssue(os.Args[2:])
	case "serve":
		err = cmdServe(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "license-tool: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "license-tool:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Usage:
  license-tool keygen -out <privkey-file>
      Generates a new Ed25519 vendor keypair. Prints the PUBLIC key (base64)
      to stdout — paste it into ProductionPublicKeyB64 in
      backend/internal/platform/license/license.go and rebuild every product
      binary. Writes the PRIVATE key (base64) to -out; keep it offline,
      outside version control, and back it up somewhere a lost laptop can't
      take out the whole company's ability to issue licenses.

  license-tool issue
      With NO flags on an interactive terminal, walks you through the fields
      with prompts and a tier menu (recommended — you can't forget a flag).

  license-tool issue -key <privkey-file> -keyid <id> -licensee <name>
                      -tier <tier> -max-subscribers <n> -version <entitled>
                      -fingerprint <fingerprint> [-out <file.json>]
      Non-interactive form (for CI/scripts). Signs a license for the given
      fingerprint (as printed by the buyer's wizard/Settings > System). Writes
      the {"payload":...,"signature":...} blob to -out (default: stdout) —
      that file is what the buyer uploads. Both forms produce byte-identical
      output for identical inputs.

  license-tool serve -key <privkey-file> -keyid <id> [-addr 127.0.0.1:7070]
      Starts a local web issuer: open the printed URL in a browser and fill in
      the licensee / tier / fingerprint fields on a form, then download the
      signed license file. The private key never leaves this machine (it is
      read here, server-side) and the listener is loopback-only unless you pass
      -allow-remote. Same signing path as issue, so output is identical.
`)
}

func cmdKeygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fs.String("out", "", "file to write the base64 private key to (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return fmt.Errorf("-out is required (do not print the private key to stdout)")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, []byte(base64.StdEncoding.EncodeToString(priv)), 0o600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}
	fmt.Println("Public key (embed as ProductionPublicKeyB64):")
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
	fmt.Printf("\nPrivate key written to %s (mode 0600). Guard it like a signing certificate.\n", *out)
	return nil
}

// issueInput is the fully-resolved set of fields to sign, produced by either
// the flag parser or the interactive prompt flow. Both paths hand it to
// signAndWrite so the two produce byte-identical output for identical inputs
// (the FR-103 hard invariant, checked in the test).
type issueInput struct {
	keyFile, keyID, licensee, tier, version, fingerprint, out string
	maxSubs                                                   int
	issuedAt                                                  time.Time

	// --- v2 fields (FR-122). Zero values (nil/""/empty) mean "not set" and
	// signBlob leaves schemaVersion at 0 (omitted -> v1) when every one of
	// them is unset, so an old-style call site keeps emitting byte-identical
	// v1 output. Setting any one of them switches the emitted payload to v2. ---
	entitlements []string
	expiresAt    *time.Time
	supportUntil *time.Time
	serial       string
}

func cmdIssue(args []string) error {
	// No flags on a terminal → interactive. No flags with no terminal (a
	// pipe/CI with nothing passed) is an explicit error, never a silent hang
	// waiting on stdin. Any flag present → the existing non-interactive path.
	if len(args) == 0 {
		if !stdinIsTerminal() {
			return fmt.Errorf("issue: no flags given and stdin is not a terminal — pass flags (see -h) or run interactively")
		}
		in, err := promptIssue()
		if err != nil {
			return err
		}
		return signAndWrite(in)
	}

	fs := flag.NewFlagSet("issue", flag.ExitOnError)
	keyFile := fs.String("key", "", "path to the base64 private key file from keygen (required)")
	keyID := fs.String("keyid", "", "vendor-assigned key id, e.g. K-2026-0001 (required)")
	licensee := fs.String("licensee", "", "licensee/ISP name (required)")
	tier := fs.String("tier", "", "subscriber tier label, e.g. pro (required)")
	maxSubs := fs.Int("max-subscribers", 0, "max-subscriber entitlement (required, >=0; 0 = unlimited)")
	version := fs.String("version", "1", "entitled major version")
	fingerprint := fs.String("fingerprint", "", "the buyer's server fingerprint (required)")
	out := fs.String("out", "", "output file (default: stdout)")
	entitlements := fs.String("entitlements", "", "comma-separated entitlement names (FR-122, e.g. white_label,self_service) — setting this or any of -expires/-support-until/-serial emits a v2 payload; omit all four for a plain v1 blob")
	expires := fs.String("expires", "", "RFC3339 expiry (e.g. 2027-01-01T00:00:00Z); blank = perpetual")
	supportUntil := fs.String("support-until", "", "RFC3339 support-window end; blank = unset (informational only, never gates)")
	serial := fs.String("serial", "", "vendor-assigned serial/order number")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for name, v := range map[string]string{"key": *keyFile, "keyid": *keyID, "licensee": *licensee, "tier": *tier, "fingerprint": *fingerprint} {
		if v == "" {
			return fmt.Errorf("-%s is required", name)
		}
	}
	if *maxSubs < 0 {
		return fmt.Errorf("-max-subscribers must be >= 0 (0 = unlimited)")
	}
	expiresAt, err := parseOptionalTime(*expires)
	if err != nil {
		return fmt.Errorf("-expires: %w", err)
	}
	supportUntilAt, err := parseOptionalTime(*supportUntil)
	if err != nil {
		return fmt.Errorf("-support-until: %w", err)
	}
	return signAndWrite(issueInput{
		keyFile: *keyFile, keyID: *keyID, licensee: *licensee, tier: *tier,
		maxSubs: *maxSubs, version: *version, fingerprint: *fingerprint, out: *out,
		issuedAt:     time.Now().UTC(),
		entitlements: splitEntitlements(*entitlements),
		expiresAt:    expiresAt,
		supportUntil: supportUntilAt,
		serial:       *serial,
	})
}

// splitEntitlements parses a comma-separated -entitlements flag value into a
// trimmed, non-empty string slice (nil for a blank input, so signBlob's
// "did the caller set any v2 field" check works on len(), not on comma
// splitting quirks like a single empty string).
func splitEntitlements(csv string) []string {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseOptionalTime parses an RFC3339 timestamp, treating a blank string as
// "unset" (nil) rather than an error — both -expires and -support-until are
// optional.
func parseOptionalTime(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, fmt.Errorf("not RFC3339 (e.g. 2027-01-01T00:00:00Z): %w", err)
	}
	return &t, nil
}

// signAndWrite is the single signing path shared by flag mode and interactive
// mode. It keeps the deliberate json.Marshal (not MarshalIndent) invariant:
// Indent would reformat the nested json.RawMessage payload, changing the exact
// bytes under the signature so the receiving Verify would (correctly) reject
// it. Found by testing end-to-end against a live server, not by inspection.
func signAndWrite(in issueInput) error {
	enc, err := signBlob(in)
	if err != nil {
		return err
	}
	if in.out == "" {
		fmt.Println(string(enc))
		return nil
	}
	return os.WriteFile(in.out, enc, 0o644)
}

// signBlob is the pure signing core: it turns a resolved issueInput into the
// marshaled {"payload":...,"signature":...} bytes and returns them. Both the
// CLI (signAndWrite) and the web issuer (serve) call it, so every path produces
// byte-identical output for identical inputs (the FR-103 invariant). It keeps
// the deliberate json.Marshal (not MarshalIndent) rule: Indent would reformat
// the nested json.RawMessage payload, changing the exact bytes under the
// signature so the receiving Verify would (correctly) reject it.
func signBlob(in issueInput) ([]byte, error) {
	privB64, err := os.ReadFile(in.keyFile)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	privRaw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(privB64)))
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	if len(privRaw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("private key is %d bytes, want %d — wrong file?", len(privRaw), ed25519.PrivateKeySize)
	}
	priv := ed25519.PrivateKey(privRaw)

	issued := in.issuedAt
	if issued.IsZero() {
		issued = time.Now().UTC()
	}
	// schemaVersion stays 0 (omitted -> v1, per Payload's omitempty tags)
	// unless the caller set at least one v2 field — so a caller that never
	// touches entitlements/expiry/support/serial keeps emitting byte-identical
	// v1 output (FR-103's invariant, now extended across schema versions too).
	schemaVersion := 0
	if len(in.entitlements) > 0 || in.expiresAt != nil || in.supportUntil != nil || in.serial != "" {
		schemaVersion = 2
	}
	p := payload{
		KeyID:           in.keyID,
		Licensee:        in.licensee,
		IssuedAt:        issued,
		Tier:            in.tier,
		MaxSubscribers:  in.maxSubs,
		EntitledVersion: in.version,
		Fingerprint:     in.fingerprint,
		Version:         schemaVersion,
		Entitlements:    in.entitlements,
		ExpiresAt:       in.expiresAt,
		SupportUntil:    in.supportUntil,
		Serial:          in.serial,
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	sig := ed25519.Sign(priv, raw)
	b := blob{Payload: raw, Signature: base64.StdEncoding.EncodeToString(sig)}
	return json.Marshal(b)
}

// stdinIsTerminal reports whether stdin is an interactive character device
// (a tty), without pulling in golang.org/x/term (which would add a dependency
// to this deliberately-tiny standalone module).
func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// promptIssue walks the vendor through the license fields on a terminal.
// Prompts go to stderr so a `> file.json` redirect of stdout still captures
// only the blob when the operator leaves the output path blank.
func promptIssue() (issueInput, error) {
	r := bufio.NewReader(os.Stdin)
	fmt.Fprintln(os.Stderr, "HikRAD license issuer (interactive). Blank output path prints the blob to stdout.")

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
	fingerprint, err := promptRequired(r, "Buyer's server fingerprint (from their wizard/Settings)")
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
	serial, err := promptWithDefault(r, "Serial / order number (blank = unset)", "")
	if err != nil {
		return issueInput{}, err
	}
	out, err := promptWithDefault(r, "Output file (blank = stdout)", "")
	if err != nil {
		return issueInput{}, err
	}
	return issueInput{
		keyFile: keyFile, keyID: keyID, licensee: licensee, tier: tier,
		maxSubs: maxSubs, version: version, fingerprint: fingerprint, out: out,
		issuedAt:     time.Now().UTC(),
		entitlements: entitlements,
		expiresAt:    expiresAt,
		supportUntil: supportUntil,
		serial:       serial,
	}, nil
}

// promptEntitlements shows the FR-122 catalogue with defaultEnts pre-checked
// (marked with a *) and lets the operator type a comma-separated list of
// numbers to toggle, or press Enter to accept the defaults as-is.
func promptEntitlements(r *bufio.Reader, defaultEnts []string) ([]string, error) {
	selected := map[string]bool{}
	for _, e := range defaultEnts {
		selected[e] = true
	}
	fmt.Fprintln(os.Stderr, "Entitlements (checked by name, never by tier — Decision 62):")
	for i, e := range knownEntitlements {
		mark := " "
		if selected[e] {
			mark = "*"
		}
		fmt.Fprintf(os.Stderr, "  [%s] %d) %s\n", mark, i+1, e)
	}
	choice, err := promptWithDefault(r, fmt.Sprintf("Toggle numbers (comma-separated, blank = accept the %s above)", map[bool]string{true: "defaults marked *", false: "empty set"}[len(defaultEnts) > 0]), "")
	if err != nil {
		return nil, err
	}
	choice = strings.TrimSpace(choice)
	if choice == "" {
		return defaultEnts, nil
	}
	for _, tok := range strings.Split(choice, ",") {
		n, convErr := strconv.Atoi(strings.TrimSpace(tok))
		if convErr != nil || n < 1 || n > len(knownEntitlements) {
			fmt.Fprintf(os.Stderr, "  ignoring invalid selection %q\n", tok)
			continue
		}
		e := knownEntitlements[n-1]
		selected[e] = !selected[e]
	}
	var out []string
	for _, e := range knownEntitlements {
		if selected[e] {
			out = append(out, e)
		}
	}
	return out, nil
}

// promptTier prints the FR-105 catalog as a numbered menu and returns the
// chosen tier label + its enforced max-subscriber count. The last option is a
// free-form custom size for one-off deals the catalog doesn't cover.
func promptTier(r *bufio.Reader) (string, int, []string, error) {
	fmt.Fprintln(os.Stderr, "Tier:")
	for i, t := range tierCatalog {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, t.Label)
	}
	fmt.Fprintf(os.Stderr, "  %d) Custom (enter tier label + max-subscribers)\n", len(tierCatalog)+1)
	for {
		choice, err := promptRequired(r, fmt.Sprintf("Select 1-%d", len(tierCatalog)+1))
		if err != nil {
			return "", 0, nil, err
		}
		n, convErr := strconv.Atoi(choice)
		if convErr != nil || n < 1 || n > len(tierCatalog)+1 {
			fmt.Fprintln(os.Stderr, "  please enter a number from the list")
			continue
		}
		if n <= len(tierCatalog) {
			t := tierCatalog[n-1]
			return t.Key, t.Max, t.DefaultEntitlements, nil
		}
		// Custom
		label, err := promptRequired(r, "Custom tier label")
		if err != nil {
			return "", 0, nil, err
		}
		maxStr, err := promptRequired(r, "Max subscribers (>=0; 0 = unlimited)")
		if err != nil {
			return "", 0, nil, err
		}
		max, convErr := strconv.Atoi(maxStr)
		if convErr != nil || max < 0 {
			fmt.Fprintln(os.Stderr, "  max-subscribers must be an integer >= 0")
			continue
		}
		return label, max, nil, nil
	}
}

func promptRequired(r *bufio.Reader, label string) (string, error) {
	for {
		fmt.Fprintf(os.Stderr, "%s: ", label)
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("read %q: %w", label, err)
		}
		if v := strings.TrimSpace(line); v != "" {
			return v, nil
		}
		fmt.Fprintln(os.Stderr, "  this field is required")
	}
}

func promptWithDefault(r *bufio.Reader, label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(os.Stderr, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(os.Stderr, "%s: ", label)
	}
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("read %q: %w", label, err)
	}
	if v := strings.TrimSpace(line); v != "" {
		return v, nil
	}
	return def, nil
}
