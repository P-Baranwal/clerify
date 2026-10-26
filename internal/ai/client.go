package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Client talks to any OpenAI-compatible endpoint, or to Anthropic's messages
// API. Nothing vendor-specific lives outside the preset table.
type Client struct {
	cfg  Config
	HTTP *http.Client
}

// New builds a client, applying defaults.
func New(cfg Config) *Client {
	cfg.Normalize()
	return &Client{
		cfg:  cfg,
		HTTP: &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second},
	}
}

// Config returns the effective configuration.
func (c *Client) Config() Config { return c.cfg }

// KeyEnvName exposes the environment variable that would supply the key.
func (c *Client) KeyEnvName() string { return keyEnvName(c.cfg) }

// Analyze sends one chat completion request and returns the model's text.
// feature is accepted for parity with the web app's call sites; it has no
// wire effect.
func (c *Client) Analyze(ctx context.Context, system, user, feature string) (string, error) {
	key := ResolveKey(c.cfg, "")
	if key == "" {
		return "", &ConfigError{Message: fmt.Sprintf("no API key configured; set %s or run `clerify ai key`", keyEnvName(c.cfg))}
	}
	if strings.TrimSpace(c.cfg.BaseURL) == "" {
		return "", &ConfigError{Message: "no base URL configured; run `clerify ai preset <name>` or set --base-url"}
	}
	if strings.TrimSpace(c.cfg.Model) == "" {
		return "", &ConfigError{Message: "no model configured; run `clerify ai preset <name>` or set --model"}
	}

	attempts := c.cfg.Retries + 1
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			backoff := time.Duration(1<<uint(i-1)) * time.Second
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(backoff):
			}
		}
		out, err := c.do(ctx, system, user, key)
		if err == nil {
			return out, nil
		}
		lastErr = err
		if !IsRetryable(err) {
			break
		}
	}
	return "", lastErr
}

// Test performs a minimal round-trip and reports latency.
func (c *Client) Test(ctx context.Context) (time.Duration, error) {
	start := time.Now()
	out, err := c.Analyze(ctx, "You are a health check. Reply with the single word OK.", "ping", "health")
	if err != nil {
		return time.Since(start), err
	}
	if strings.TrimSpace(out) == "" {
		return time.Since(start), errors.New("provider returned an empty response")
	}
	return time.Since(start), nil
}

func (c *Client) do(ctx context.Context, system, user, key string) (string, error) {
	endpoint := strings.TrimRight(c.cfg.BaseURL, "/")

	headers := map[string]string{"Content-Type": "application/json"}
	var body map[string]any

	if c.cfg.Wire == WireAnthropic {
		endpoint += "/messages"
		body = map[string]any{
			"model":       c.cfg.Model,
			"max_tokens":  c.cfg.MaxTokens,
			"temperature": c.cfg.Temperature,
			"system":      system,
			"messages": []map[string]string{
				{"role": "user", "content": user},
			},
		}
		headers["x-api-key"] = key
		headers["anthropic-version"] = "2023-06-01"
	} else {
		endpoint += "/chat/completions"
		body = map[string]any{
			"model":       c.cfg.Model,
			"temperature": c.cfg.Temperature,
			"max_tokens":  c.cfg.MaxTokens,
			"messages": []map[string]string{
				{"role": "system", "content": system},
				{"role": "user", "content": user},
			},
		}
		if c.wantJSONMode() {
			body["response_format"] = map[string]string{"type": "json_object"}
		}
		if c.cfg.AuthHeader != "" {
			value := key
			if c.cfg.AuthPrefix != "" {
				value = c.cfg.AuthPrefix + " " + key
			}
			headers[c.cfg.AuthHeader] = value
		} else {
			headers["Authorization"] = "Bearer " + key
		}
	}

	for k, v := range c.cfg.ExtraBody {
		if _, exists := body[k]; !exists {
			body[k] = v
		}
	}
	for k, v := range c.cfg.ExtraHeaders {
		headers[k] = v
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("call %s: %w", endpoint, err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		apiErr := normalizeErrorBody(res.StatusCode, raw)
		var probe struct {
			ResetAt string `json:"resetDate"`
			Reset   string `json:"reset_at"`
		}
		if jsonUnmarshal(raw, &probe) == nil {
			if probe.ResetAt != "" {
				apiErr.ResetAt = probe.ResetAt
			} else if probe.Reset != "" {
				apiErr.ResetAt = probe.Reset
			}
		}
		return "", apiErr
	}

	if c.cfg.Wire == WireAnthropic {
		return parseAnthropicResponse(raw)
	}
	return parseOpenAIResponse(raw)
}

// ListModels queries /models when the provider supports it. Providers without
// the endpoint return an error, which the CLI reports as unsupported.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	key := ResolveKey(c.cfg, "")
	endpoint := strings.TrimRight(c.cfg.BaseURL, "/") + "/models"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{"Content-Type": "application/json"}
	if c.cfg.Wire == WireAnthropic {
		headers["x-api-key"] = key
		headers["anthropic-version"] = "2023-06-01"
	} else if c.cfg.AuthHeader != "" {
		value := key
		if c.cfg.AuthPrefix != "" {
			value = c.cfg.AuthPrefix + " " + key
		}
		headers[c.cfg.AuthHeader] = value
	} else {
		headers["Authorization"] = "Bearer " + key
	}
	for k, v := range c.cfg.ExtraHeaders {
		headers[k] = v
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, normalizeErrorBody(res.StatusCode, raw)
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := jsonUnmarshal(raw, &payload); err != nil {
		return nil, &ParseError{Message: "provider returned a non-JSON model list"}
	}
	out := make([]string, 0, len(payload.Data)+len(payload.Models))
	for _, m := range payload.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	for _, m := range payload.Models {
		if m.Name != "" {
			out = append(out, m.Name)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, &ParseError{Message: "provider returned no models"}
	}
	return out, nil
}

func (c *Client) wantJSONMode() bool {
	switch strings.ToLower(c.cfg.ResponseFormatJSON) {
	case "on", "true", "yes":
		return true
	case "off", "false", "no":
		return false
	}
	switch c.cfg.Preset {
	case "openai", "groq", "deepseek", "ollama", "lmstudio", "llamacpp", "vllm":
		return true
	}
	return false
}

func parseOpenAIResponse(raw []byte) (string, error) {
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := jsonUnmarshal(raw, &resp); err != nil {
		return "", &ParseError{Message: "provider returned a non-JSON response"}
	}
	if len(resp.Choices) == 0 {
		return "", &ParseError{Message: "provider returned no choices"}
	}
	content := resp.Choices[0].Message.Content
	if strings.TrimSpace(content) == "" {
		return "", &ParseError{Message: "provider returned an empty message"}
	}
	return content, nil
}

func parseAnthropicResponse(raw []byte) (string, error) {
	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := jsonUnmarshal(raw, &resp); err != nil {
		return "", &ParseError{Message: "provider returned a non-JSON response"}
	}
	var b strings.Builder
	for _, part := range resp.Content {
		if part.Type == "" || part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	out := b.String()
	if strings.TrimSpace(out) == "" {
		return "", &ParseError{Message: "provider returned an empty message"}
	}
	return out, nil
}

// jsonUnmarshal is a tiny indirection so errors.go and client.go share one
// decoding default without importing encoding/json twice.
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
