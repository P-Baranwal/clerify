// Package pdf extracts text from PDF and plain-text files.
package pdf

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
)

// Extract returns the text of path. PDFs use pdftotext when available (best
// layout fidelity) and fall back to a pure-Go extractor.
func Extract(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".pdf" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}

	if text, err := extractWithPdftotext(path); err == nil && strings.TrimSpace(text) != "" {
		return text, nil
	}
	return extractWithGoPDF(path)
}

func extractWithPdftotext(path string) (string, error) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		return "", fmt.Errorf("pdftotext not found")
	}
	cmd := exec.Command("pdftotext", "-layout", "-enc", "UTF-8", path, "-")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

func extractWithGoPDF(path string) (string, error) {
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
	if _, err := buf.ReadFrom(reader); err != nil {
		return "", err
	}
	text := buf.String()
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("pdf contains no extractable text (it may be a scanned image)")
	}
	return text, nil
}
