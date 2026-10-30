package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientOpenAIBearerAuthAndPayload(t *testing.T) {
	var (
		auth   string
		body   map[string]any
		method string
		path   string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		auth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello"}}]}`))
	}))
	defer srv.Close()

	cfg := Config{
		BaseURL: srv.URL, Model: "test-model", APIKey: "secret",
		AuthHeader: "Authorization", AuthPrefix: "Bearer",
		Temperature: 0.2, MaxTokens: 123, TimeoutSeconds: 5,
		ExtraBody: map[string]any{"top_k": 3},
	}
	out, err := New(cfg).Analyze(context.Background(), "sys", "usr", "test")
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello" {
		t.Errorf("out = %q", out)
	}
	if method != http.MethodPost || path != "/chat/completions" {
		t.Errorf("%s %s", method, path)
	}
	if auth != "Bearer secret" {
		t.Errorf("auth = %q", auth)
	}
	if body["model"] != "test-model" {
		t.Errorf("model = %v", body["model"])
	}
	if body["top_k"] != float64(3) {
		t.Errorf("extra body not merged: %v", body["top_k"])
	}
	messages, _ := body["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages = %v", messages)
	}
	first := messages[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "sys" {
		t.Errorf("system message = %v", first)
	}
}

func TestClientSarvamHeader(t *testing.T) {
	var subscriptionKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subscriptionKey = r.Header.Get("api-subscription-key")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	cfg := Config{
		BaseURL: srv.URL, Model: "sarvam-105b", APIKey: "sarvam-secret",
		AuthHeader: "api-subscription-key", AuthPrefix: "",
		Temperature: 0.2, MaxTokens: 10, TimeoutSeconds: 5,
	}
	if _, err := New(cfg).Analyze(context.Background(), "s", "u", "f"); err != nil {
		t.Fatal(err)
	}
	if subscriptionKey != "sarvam-secret" {
		t.Errorf("api-subscription-key = %q", subscriptionKey)
	}
}

func TestClientAnthropicWireShape(t *testing.T) {
	var (
		path    string
		apiKey  string
		version string
		body    map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		apiKey = r.Header.Get("x-api-key")
		version = r.Header.Get("anthropic-version")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"from claude"}]}`))
	}))
	defer srv.Close()

	cfg := Config{
		Wire: WireAnthropic, BaseURL: srv.URL, Model: "claude", APIKey: "k",
		AuthHeader: "x-api-key", Temperature: 0.2, MaxTokens: 10, TimeoutSeconds: 5,
	}
	out, err := New(cfg).Analyze(context.Background(), "system text", "user text", "f")
	if err != nil {
		t.Fatal(err)
	}
	if out != "from claude" {
		t.Errorf("out = %q", out)
	}
	if path != "/messages" {
		t.Errorf("path = %q", path)
	}
	if apiKey != "k" || version == "" {
		t.Errorf("headers = %q %q", apiKey, version)
	}
	if body["system"] != "system text" {
		t.Errorf("system should be top-level: %v", body["system"])
	}
	if _, ok := body["messages"].([]any); !ok {
		t.Errorf("messages = %v", body["messages"])
	}
}

func TestClientErrorExtraction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key","type":"auth_error"}}`))
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Model: "m", APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer", TimeoutSeconds: 5, MaxTokens: 10}
	_, err := New(cfg).Analyze(context.Background(), "s", "u", "f")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("err = %v", err)
	}
	if IsRetryable(err) {
		t.Errorf("401 should not be retryable")
	}
}

func TestClientRetriesServerErrors(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"second try"}}]}`))
	}))
	defer srv.Close()

	cfg := Config{
		BaseURL: srv.URL, Model: "m", APIKey: "k",
		AuthHeader: "Authorization", AuthPrefix: "Bearer",
		TimeoutSeconds: 10, MaxTokens: 10, Retries: 1,
	}
	out, err := New(cfg).Analyze(context.Background(), "s", "u", "f")
	if err != nil {
		t.Fatal(err)
	}
	if out != "second try" || calls != 2 {
		t.Errorf("out = %q, calls = %d", out, calls)
	}
}

func TestResolveKeyPrecedence(t *testing.T) {
	cfg := Config{Preset: "sarvam", APIKey: "from-config"}

	if got := ResolveKey(cfg, "from-flag"); got != "from-flag" {
		t.Errorf("flag should win: %q", got)
	}

	t.Setenv("CLERIFY_API_KEY", "from-env")
	if got := ResolveKey(cfg, ""); got != "from-env" {
		t.Errorf("env should win: %q", got)
	}

	t.Setenv("CLERIFY_API_KEY", "")
	t.Setenv("SARVAM_API_KEY", "from-preset-env")
	if got := ResolveKey(cfg, ""); got != "from-preset-env" {
		t.Errorf("preset env should win: %q", got)
	}

	t.Setenv("SARVAM_API_KEY", "")
	if got := ResolveKey(cfg, ""); got != "from-config" {
		t.Errorf("config should be last: %q", got)
	}
}

func TestApplyPresetsCoverBothWires(t *testing.T) {
	if p, ok := LookupPreset("sarvam"); !ok || p.AuthHeader != "api-subscription-key" || p.AuthPrefix != "" {
		t.Errorf("sarvam preset = %+v", p)
	}
	if p, ok := LookupPreset("openai"); !ok || p.AuthHeader != "Authorization" || p.AuthPrefix != "Bearer" {
		t.Errorf("openai preset = %+v", p)
	}
	if p, ok := LookupPreset("anthropic"); !ok || p.Wire != WireAnthropic {
		t.Errorf("anthropic preset = %+v", p)
	}
	if _, ok := LookupPreset("nope"); ok {
		t.Errorf("unknown preset should not resolve")
	}
}

func TestNoAuthSkipsKeyAndHeader(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	cfg := Config{
		Preset: "ollama", BaseURL: srv.URL, Model: "local", NoAuth: true,
		Temperature: 0.2, MaxTokens: 10, TimeoutSeconds: 5,
	}
	if _, err := New(cfg).Analyze(context.Background(), "s", "u", "f"); err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		t.Errorf("local preset sent an auth header: %q", auth)
	}
}

func TestApplyPresetsDoNotInheritSarvamExtraBody(t *testing.T) {
	sarvam, ok := ApplyPreset("sarvam")
	if !ok {
		t.Fatal("sarvam preset missing")
	}
	if _, exists := sarvam.ExtraBody["reasoning_effort"]; !exists {
		t.Error("sarvam should keep reasoning_effort")
	}
	for _, name := range []string{"anthropic", "openai", "gemini", "ollama"} {
		cfg, ok := ApplyPreset(name)
		if !ok {
			t.Fatalf("%s preset missing", name)
		}
		if cfg.ExtraBody != nil {
			t.Errorf("%s inherited extra_body: %#v", name, cfg.ExtraBody)
		}
	}
}

func TestConfigNormalizeNoAuth(t *testing.T) {
	cfg := Config{NoAuth: true}
	cfg.Normalize()
	if cfg.AuthHeader != "" || cfg.AuthPrefix != "" {
		t.Errorf("NoAuth config gained auth defaults: %+v", cfg)
	}
}
