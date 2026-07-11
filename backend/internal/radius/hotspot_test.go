package radius

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

func unzip(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(b)
	}
	return out
}

func TestBuildHotspotPackage_Themed(t *testing.T) {
	b := brandingSettings{Name: "Tigris Net", ColorPrimary: "#e11d48"}.withDefaults()
	pkg, err := buildHotspotPackage(b, []string{"portal.tigris.iq", "pay.tigris.iq"})
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, pkg)

	for _, want := range []string{"login.html", "style.css", "md5.js", "README.txt"} {
		if _, ok := files[want]; !ok {
			t.Errorf("missing %s in package", want)
		}
	}
	login := files["login.html"]
	if !strings.Contains(login, "Tigris Net") {
		t.Error("login.html not themed with ISP name")
	}
	if !strings.Contains(login, "$(link-login-only)") {
		t.Error("login.html missing MikroTik login action var")
	}
	if !strings.Contains(login, `name="code"`) {
		t.Error("login.html missing voucher code field")
	}
	if !strings.Contains(login, "doVoucher") || !strings.Contains(login, "doLogin") {
		t.Error("login.html missing login handlers")
	}
	if !strings.Contains(files["style.css"], "#e11d48") {
		t.Error("style.css missing brand colour")
	}
	if !strings.Contains(files["md5.js"], "function hexMD5") {
		t.Error("md5.js missing hexMD5")
	}
	if !strings.Contains(files["README.txt"], "portal.tigris.iq") {
		t.Error("README missing walled-garden host")
	}
}

func TestBuildHotspotPackage_Defaults(t *testing.T) {
	pkg, err := buildHotspotPackage(brandingSettings{}.withDefaults(), nil)
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, pkg)
	if !strings.Contains(files["login.html"], "HikRAD Wi-Fi") {
		t.Error("default ISP name missing")
	}
	if !strings.Contains(files["style.css"], "#2563eb") {
		t.Error("default colour missing")
	}
}

func TestCSSColor_RejectsInjection(t *testing.T) {
	cases := map[string]string{
		"#e11d48":            "#e11d48",
		"rgb(10, 20, 30)":    "rgb(10, 20, 30)",
		"red":                "red",
		"":                   "#2563eb",
		"}body{display:none": "#2563eb", // injection attempt rejected
		"#fff;background:url(x)": "#2563eb",
	}
	for in, want := range cases {
		if got := cssColor(in); got != want {
			t.Errorf("cssColor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSafeFilename(t *testing.T) {
	cases := map[string]string{
		"Main Router":  "Main-Router",
		"nas/../etc":   "nas----etc",
		"":             "nas",
		"ap_01":        "ap_01",
	}
	for in, want := range cases {
		if got := safeFilename(in); got != want {
			t.Errorf("safeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHTMLEscape(t *testing.T) {
	if got := htmlEscape(`a<b>&"'`); got != "a&lt;b&gt;&amp;&quot;&#39;" {
		t.Errorf("htmlEscape = %q", got)
	}
}
