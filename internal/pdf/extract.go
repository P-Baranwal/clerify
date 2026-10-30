// Package pdf extracts text from PDF and plain-text files.
package pdf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"
)

const (
	maxTextBytes     = 64 << 20
	pdftotextTimeout = 30 * time.Second
)

// Extract returns the text of path. PDFs use pdftotext when available (best
// layout fidelity) and fall back to a pure-Go extractor. Non-PDF input is read
// as bounded text and rejected if it looks binary.
func Extract(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".pdf" {
		return readTextFile(path)
	}

	if text, err := extractWithPdftotext(path); err == nil && strings.TrimSpace(text) != "" {
		return text, nil
	}
	return extractWithGoPDF(path)
}

func readTextFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > maxTextBytes {
		return "", fmt.Errorf("%s is too large to read as text (%d bytes)", path, info.Size())
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxTextBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxTextBytes {
		return "", fmt.Errorf("%s is too large to read as text", path)
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", fmt.Errorf("%s looks like a binary file, not text", path)
	}
	contentType := http.DetectContentType(data)
	if !strings.HasPrefix(contentType, "text/") &&
		contentType != "application/json" &&
		contentType != "application/xml" {
		return "", fmt.Errorf("%s looks like %s, not text", path, contentType)
	}
	return string(data), nil
}

func extractWithPdftotext(path string) (string, error) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		return "", fmt.Errorf("pdftotext not found")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	ctx, cancel := context.WithTimeout(context.Background(), pdftotextTimeout)
	defer cancel()

	// An absolute path can never be mistaken for an option such as -layout.
	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", "-enc", "UTF-8", abs, "-")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("pdftotext timed out after %s", pdftotextTimeout)
		}
		return "", err
	}
	return out.String(), nil
}

func extractWithGoPDF(path string) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text = ""
			err = fmt.Errorf("pdf parser failed: %v", r)
		}
	}()

	f, r, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("read pdf (install poppler-utils for better results): %w", err)
	}
	defer f.Close()

	reader, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("extract pdf text: %w", err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(io.LimitReader(reader, maxTextBytes+1)); err != nil {
		return "", err
	}
	if buf.Len() > maxTextBytes {
		return "", fmt.Errorf("extracted pdf text is too large")
	}
	text = buf.String()
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("pdf contains no extractable text (it may be a scanned image)")
	}
	return text, nil
}
