package monitorsvc

// FR-55/FR-59 subscriber WhatsApp delivery: template resolution, credential
// gating, and an end-to-end proof against a request-capture fake HTTP server
// standing in for the Meta Graph API (gate item 9's fallback for when Meta
// onboarding is pending).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hikrad/hikrad/internal/platform"
)

type fakeSettings struct {
	mu sync.Mutex
	m  map[string]json.RawMessage
}

func newFakeSettings() *fakeSettings { return &fakeSettings{m: map[string]json.RawMessage{}} }

func (f *fakeSettings) GetRaw(_ context.Context, key string) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.m[key]
	if !ok {
		return nil, platform.ErrSettingNotFound
	}
	return v, nil
}

func (f *fakeSettings) Set(_ context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.m[key] = raw
	f.mu.Unlock()
	return nil
}

func (f *fakeSettings) Invalidate(key string) {
	f.mu.Lock()
	delete(f.m, key)
	f.mu.Unlock()
}

func (f *fakeSettings) OnChange(func(string)) {}

func TestResolveTemplate_FallsBackToEnglish(t *testing.T) {
	cat := templateCatalog{Templates: map[string]map[string]string{
		"payment_receipt": {"en": "payment_receipt_en", "ar": "payment_receipt_ar"},
	}}
	if name, ok := resolveTemplate(cat, "payment_receipt", "ar"); !ok || name != "payment_receipt_ar" {
		t.Fatalf("ar lookup = %q, %v", name, ok)
	}
	// ku has no registered template: falls back to en rather than dropping.
	if name, ok := resolveTemplate(cat, "payment_receipt", "ku"); !ok || name != "payment_receipt_en" {
		t.Fatalf("ku fallback = %q, %v", name, ok)
	}
	if _, ok := resolveTemplate(cat, "unknown_key", "en"); ok {
		t.Fatal("unknown template key should not resolve")
	}
}

// Missing credentials/template config must degrade silently (NFR-7), not error.
func TestDeliverSubscriberWhatsApp_DegradesWhenUnconfigured(t *testing.T) {
	settings := newFakeSettings()
	if err := deliverSubscriberWhatsApp(context.Background(), settings, httpClient(), "9647700000000", "ar", "payment_receipt", []string{"1"}); err != nil {
		t.Fatalf("expected nil (degrade, not error) when unconfigured, got %v", err)
	}
}

// End-to-end proof against a request-capture fake (gate item 9 fallback):
// resolves credentials + template + language and posts the exact Meta
// template-message shape.
func TestDeliverSubscriberWhatsApp_RequestCaptureFake(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing/wrong bearer token: %q", r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	origBase := graphAPIBase
	graphAPIBase = srv.URL
	defer func() { graphAPIBase = origBase }()

	settings := newFakeSettings()
	if err := settings.Set(context.Background(), "notifications.whatsapp", whatsAppConfig{Token: "test-token", PhoneID: "12345"}); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), subscriberTemplatesKey, templateCatalog{Templates: map[string]map[string]string{
		"payment_receipt": {"ar": "payment_receipt_ar"},
	}}); err != nil {
		t.Fatal(err)
	}

	err := deliverSubscriberWhatsApp(context.Background(), settings, httpClient(), "9647700000000", "ar", "payment_receipt", []string{"15000", "R-042", "2026-08-01"})
	if err != nil {
		t.Fatalf("deliverSubscriberWhatsApp: %v", err)
	}
	if captured == nil {
		t.Fatal("fake server never received a request")
	}
	if captured["to"] != "9647700000000" {
		t.Fatalf("to = %v, want 9647700000000", captured["to"])
	}
	tmpl, _ := captured["template"].(map[string]any)
	if tmpl["name"] != "payment_receipt_ar" {
		t.Fatalf("template name = %v, want payment_receipt_ar", tmpl["name"])
	}
	lang, _ := tmpl["language"].(map[string]any)
	if lang["code"] != "ar" {
		t.Fatalf("language code = %v, want ar", lang["code"])
	}
	components, _ := tmpl["components"].([]any)
	if len(components) != 1 {
		t.Fatalf("expected 1 body component, got %d", len(components))
	}
	body, _ := components[0].(map[string]any)
	params, _ := body["parameters"].([]any)
	if len(params) != 3 {
		t.Fatalf("expected 3 body params, got %d: %v", len(params), params)
	}
	first, _ := params[0].(map[string]any)
	if first["text"] != "15000" {
		t.Fatalf("first param = %v, want 15000", first["text"])
	}
}
