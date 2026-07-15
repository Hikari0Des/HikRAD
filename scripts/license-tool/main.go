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
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
)

// payload mirrors internal/platform/license.Payload exactly.
type payload struct {
	KeyID           string    `json:"key_id"`
	Licensee        string    `json:"licensee"`
	IssuedAt        time.Time `json:"issued_at"`
	Tier            string    `json:"tier"`
	MaxSubscribers  int       `json:"max_subscribers"`
	EntitledVersion string    `json:"entitled_version"`
	Fingerprint     string    `json:"fingerprint"`
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

  license-tool issue -key <privkey-file> -keyid <id> -licensee <name>
                      -tier <tier> -max-subscribers <n> -version <entitled>
                      -fingerprint <fingerprint> [-out <file.json>]
      Signs a license for the given fingerprint (as printed by the buyer's
      wizard/Settings > System). Writes the {"payload":...,"signature":...}
      blob to -out (default: stdout) — that file is what the buyer uploads.
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

func cmdIssue(args []string) error {
	fs := flag.NewFlagSet("issue", flag.ExitOnError)
	keyFile := fs.String("key", "", "path to the base64 private key file from keygen (required)")
	keyID := fs.String("keyid", "", "vendor-assigned key id, e.g. K-2026-0001 (required)")
	licensee := fs.String("licensee", "", "licensee/ISP name (required)")
	tier := fs.String("tier", "", "subscriber tier label, e.g. 5k (required)")
	maxSubs := fs.Int("max-subscribers", 0, "max-subscriber entitlement (required, >0)")
	version := fs.String("version", "1", "entitled major version")
	fingerprint := fs.String("fingerprint", "", "the buyer's server fingerprint (required)")
	out := fs.String("out", "", "output file (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for name, v := range map[string]string{"key": *keyFile, "keyid": *keyID, "licensee": *licensee, "tier": *tier, "fingerprint": *fingerprint} {
		if v == "" {
			return fmt.Errorf("-%s is required", name)
		}
	}
	if *maxSubs <= 0 {
		return fmt.Errorf("-max-subscribers must be > 0")
	}

	privB64, err := os.ReadFile(*keyFile)
	if err != nil {
		return fmt.Errorf("read private key: %w", err)
	}
	privRaw, err := base64.StdEncoding.DecodeString(string(privB64))
	if err != nil {
		return fmt.Errorf("decode private key: %w", err)
	}
	if len(privRaw) != ed25519.PrivateKeySize {
		return fmt.Errorf("private key is %d bytes, want %d — wrong file?", len(privRaw), ed25519.PrivateKeySize)
	}
	priv := ed25519.PrivateKey(privRaw)

	p := payload{
		KeyID:           *keyID,
		Licensee:        *licensee,
		IssuedAt:        time.Now().UTC(),
		Tier:            *tier,
		MaxSubscribers:  *maxSubs,
		EntitledVersion: *version,
		Fingerprint:     *fingerprint,
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(priv, raw)
	b := blob{Payload: raw, Signature: base64.StdEncoding.EncodeToString(sig)}

	// Deliberately json.Marshal, not MarshalIndent: Indent reformats nested
	// json.RawMessage content too, which would change payload's bytes from
	// what was actually signed — the receiving Verify would then (correctly)
	// reject it. Found by testing this tool end-to-end against a live
	// server, not by reasoning about it — keep it compact.
	enc, err := json.Marshal(b)
	if err != nil {
		return err
	}
	if *out == "" {
		fmt.Println(string(enc))
		return nil
	}
	return os.WriteFile(*out, enc, 0o644)
}
