package ai

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ConfigError means the provider is not usable as configured.
type ConfigError struct{ Message string }

func (e *ConfigError) Error() string { return e.Message }

// APIError is a non-2xx provider response, parsed as far as possible.
type APIError struct {
	Status  int
	Code    string
	Message string
	ResetAt string
	Body    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("provider returned HTTP %d", e.Status)
}

// ParseError means the model replied with something that is not the expected
// JSON shape.
type ParseError struct{ Message string }

func (e *ParseError) Error() string { return e.Message }

// IsRetryable reports whether another attempt could plausibly succeed.
func IsRetryable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == http.StatusTooManyRequests || apiErr.Status >= 500
	}
	var cfgErr *ConfigError
	if errors.As(err, &cfgErr) {
		return false
	}
	var parseErr *ParseError
	if errors.As(err, &parseErr) {
		return false
	}
	// Network errors and timeouts are retryable.
	return true
}

func normalizeErrorBody(status int, body []byte) *APIError {
	e := &APIError{Status: status, Body: strings.TrimSpace(string(body))}

	var openAI struct {
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := jsonUnmarshal(body, &openAI); err == nil {
		switch {
		case openAI.Error != nil && openAI.Error.Message != "":
			e.Message = openAI.Error.Message
			e.Code = openAI.Error.Type
		case openAI.Message != "":
			e.Message = openAI.Message
		}
	}

	var errString struct {
		Error string `json:"error"`
	}
	if e.Message == "" {
		if err := jsonUnmarshal(body, &errString); err == nil && errString.Error != "" {
			e.Message = errString.Error
		}
	}

	if e.Message == "" && e.Body != "" {
		trimmed := e.Body
		if len(trimmed) > 400 {
			trimmed = trimmed[:400] + "…"
		}
		e.Message = fmt.Sprintf("HTTP %d: %s", status, trimmed)
	}
	return e
}
