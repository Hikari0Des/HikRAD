package main

// The graphical (web) license issuer: `license-tool serve`. It puts a browser
// form in front of the exact same signing core (signBlob) the CLI uses, so a
// vendor who doesn't want to remember flags can fill in licensee / tier /
// fingerprint and download the signed license file.
//
// Security posture: the private key is read HERE (server-side, on the vendor's
// machine) and never sent to the browser; the listener is loopback-only unless
// the operator explicitly passes -allow-remote. This is a single-operator local
// tool, not a service — there is no auth beyond "you already have the key file
// and can reach localhost".

import (
	"encoding/base64"
	"flag"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	keyFile := fs.String("key", "", "path to the base64 private key file from keygen (required)")
	keyID := fs.String("keyid", "", "vendor-assigned key id, e.g. K-2026-0001 (required)")
	addr := fs.String("addr", "127.0.0.1:7070", "listen address (loopback-only unless -allow-remote)")
	allowRemote := fs.Bool("allow-remote", false, "permit a non-loopback listen address (dangerous: exposes the signer)")
	noOpen := fs.Bool("no-open", false, "do not auto-open the browser on start")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *keyFile == "" || *keyID == "" {
		return fmt.Errorf("-key and -keyid are required")
	}
	// Fail fast if the key file is unreadable/wrong, rather than only when the
	// first issue is submitted — signBlob with a dummy input touches the file.
	if _, err := signBlob(issueInput{keyFile: *keyFile, keyID: *keyID, licensee: "x", tier: "x", fingerprint: "x", issuedAt: time.Unix(0, 0).UTC()}); err != nil {
		return fmt.Errorf("private key check failed: %w", err)
	}
	if !*allowRemote {
		host, _, err := net.SplitHostPort(*addr)
		if err != nil {
			return fmt.Errorf("parse -addr %q: %w", *addr, err)
		}
		if !isLoopbackHost(host) {
			return fmt.Errorf("-addr %q is not loopback; pass -allow-remote to expose the signer on the network (not recommended)", *addr)
		}
	}

	srv := &issuerServer{keyFile: *keyFile, keyID: *keyID}
	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleForm)
	mux.HandleFunc("/issue", srv.handleIssue)

	url := "http://" + *addr + "/"
	fmt.Printf("HikRAD license issuer running — open %s in a browser (Ctrl-C to stop).\n", url)
	if *allowRemote {
		fmt.Println("WARNING: -allow-remote is set; anyone who can reach this address can sign licenses with your key.")
	}
	if !*noOpen {
		// Best-effort convenience: pop the browser once the listener is up. A
		// failure here (headless box, no browser) is silent — the URL is printed.
		go func() {
			time.Sleep(400 * time.Millisecond)
			_ = openBrowser(url)
		}()
	}
	return http.ListenAndServe(*addr, mux)
}

// openBrowser opens url in the OS default browser. Best-effort, platform-aware;
// returns the launch error (ignored by the caller) without waiting for the
// browser process.
func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd, args = "open", []string{url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	return exec.Command(cmd, args...).Start()
}

func isLoopbackHost(host string) bool {
	if host == "localhost" || host == "" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

type issuerServer struct {
	keyFile string
	keyID   string
}

// pageData drives the single HTML template. Result/Erro/DownloadURI are only
// set after a POST; a bare GET renders just the form.
type pageData struct {
	KeyID       string
	Tiers       []tierOption
	Version     string
	Result      string       // the signed blob, shown for copy
	DownloadURI template.URL // data: URI so the browser can save the file
	Filename    string
	Erro        string
	// echo back what was entered so a validation error doesn't wipe the form
	Licensee, Tier, MaxSubs, Fingerprint string
}

func (s *issuerServer) handleForm(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	render(w, pageData{KeyID: s.keyID, Tiers: tierCatalog, Version: "1"})
}

func (s *issuerServer) handleIssue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	licensee := strings.TrimSpace(r.FormValue("licensee"))
	tier := strings.TrimSpace(r.FormValue("tier"))
	version := strings.TrimSpace(r.FormValue("version"))
	fingerprint := strings.TrimSpace(r.FormValue("fingerprint"))
	maxStr := strings.TrimSpace(r.FormValue("max_subscribers"))
	if version == "" {
		version = "1"
	}

	base := pageData{
		KeyID: s.keyID, Tiers: tierCatalog, Version: version,
		Licensee: licensee, Tier: tier, MaxSubs: maxStr, Fingerprint: fingerprint,
	}

	fail := func(msg string) {
		base.Erro = msg
		render(w, base)
	}
	if licensee == "" || tier == "" || fingerprint == "" {
		fail("Licensee, tier and fingerprint are all required.")
		return
	}
	// A catalog tier fixes its own max-subscriber count (the backend enforces
	// the number, not the label); a custom tier takes the typed value.
	maxSubs, ok := catalogMax(tier)
	if !ok {
		n, err := strconv.Atoi(maxStr)
		if err != nil || n < 0 {
			fail("For a custom tier, max-subscribers must be an integer ≥ 0 (0 = unlimited).")
			return
		}
		maxSubs = n
	}

	enc, err := signBlob(issueInput{
		keyFile: s.keyFile, keyID: s.keyID, licensee: licensee, tier: tier,
		maxSubs: maxSubs, version: version, fingerprint: fingerprint,
		issuedAt: time.Now().UTC(),
	})
	if err != nil {
		fail("Signing failed: " + err.Error())
		return
	}

	base.Result = string(enc)
	base.DownloadURI = template.URL("data:application/json;base64," + base64.StdEncoding.EncodeToString(enc))
	base.Filename = "hikrad-license-" + slugify(licensee) + ".json"
	render(w, base)
}

// catalogMax returns the enforced max-subscribers for a known tier key, and
// false if the tier is not in the catalog (a custom tier).
func catalogMax(tier string) (int, bool) {
	for _, t := range tierCatalog {
		if t.Key == tier {
			return t.Max, true
		}
	}
	return 0, false
}

func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "license"
	}
	return out
}

func render(w http.ResponseWriter, d pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTmpl.Execute(w, d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// pageTmpl is the whole UI: a form that POSTs to /issue, plus a result block
// shown after signing. Deliberately dependency-free (no external CSS/JS) so the
// tool is a single self-contained binary. The small inline script only fills
// the max-subscribers field from the chosen tier for convenience.
var pageTmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>HikRAD license issuer</title>
<style>
  :root { color-scheme: light dark; }
  body { font: 15px/1.5 system-ui, sans-serif; max-width: 640px; margin: 2rem auto; padding: 0 1rem; }
  h1 { font-size: 1.3rem; } .sub { opacity: .7; margin-top: -.5rem; }
  label { display: block; margin: .9rem 0 .2rem; font-weight: 600; }
  input, select { width: 100%; padding: .5rem .6rem; font: inherit; border: 1px solid #8886; border-radius: 8px; background: Field; color: FieldText; box-sizing: border-box; }
  select option { background: Field; color: FieldText; }
  button { margin-top: 1.2rem; padding: .6rem 1.1rem; font: inherit; font-weight: 600; border: 0; border-radius: 8px; background: #2563eb; color: #fff; cursor: pointer; }
  .err { background: #ef444422; border: 1px solid #ef444488; padding: .6rem .8rem; border-radius: 8px; margin: 1rem 0; }
  .result { margin-top: 1.5rem; border-top: 1px solid #8884; padding-top: 1rem; }
  textarea { width: 100%; height: 8rem; font: 13px monospace; border-radius: 8px; box-sizing: border-box; }
  .dl { display: inline-block; margin-top: .6rem; padding: .5rem .9rem; border-radius: 8px; background: #16a34a; color: #fff; text-decoration: none; font-weight: 600; }
  .hint { opacity: .7; font-weight: 400; font-size: .85em; }
</style></head><body>
<h1>HikRAD license issuer</h1>
<p class="sub">Key id <strong>{{.KeyID}}</strong> · signed locally, key never leaves this machine.</p>
{{if .Erro}}<div class="err">{{.Erro}}</div>{{end}}
<form method="post" action="/issue">
  <label>Licensee / ISP name</label>
  <input name="licensee" value="{{.Licensee}}" autofocus required>

  <label>Plan tier <span class="hint">(sets the subscriber cap)</span></label>
  <select name="tier" id="tier" onchange="onTier()">
    {{range .Tiers}}<option value="{{.Key}}" data-max="{{.Max}}" {{if eq $.Tier .Key}}selected{{end}}>{{.Label}}</option>{{end}}
    <option value="custom" {{if eq .Tier "custom"}}selected{{end}}>Custom — enter your own cap…</option>
  </select>

  <label>Max subscribers <span class="hint">(0 = unlimited; set automatically by the tier — editable only for Custom)</span></label>
  <input name="max_subscribers" id="max" value="{{.MaxSubs}}" inputmode="numeric">

  <label>Entitled major version</label>
  <input name="version" value="{{.Version}}">

  <label>Buyer's server fingerprint <span class="hint">(from their wizard / Settings › System)</span></label>
  <input name="fingerprint" value="{{.Fingerprint}}" dir="ltr" placeholder="abcd…:efgh…" required>

  <button type="submit">Sign license</button>
</form>

{{if .Result}}
<div class="result">
  <label>Signed license — this file is what the buyer uploads:</label>
  <textarea readonly onclick="this.select()">{{.Result}}</textarea>
  <a class="dl" href="{{.DownloadURI}}" download="{{.Filename}}">Download {{.Filename}}</a>
</div>
{{end}}

<script>
  function onTier() {
    var sel = document.getElementById('tier');
    var max = document.getElementById('max');
    var opt = sel.options[sel.selectedIndex];
    if (sel.value === 'custom') { max.readOnly = false; max.value = ''; max.focus(); }
    else { max.value = opt.getAttribute('data-max'); max.readOnly = true; }
  }
  onTier();
</script>
</body></html>`))
