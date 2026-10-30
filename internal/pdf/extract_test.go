package pdf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractRejectsBinaryText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "binary.txt")
	if err := os.WriteFile(path, []byte("hello\x00world"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(path); err == nil {
		t.Fatal("binary text file should be rejected")
	}
}

func TestExtractMalformedPDFDoesNotPanic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4\nnot really a pdf\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(path); err == nil {
		t.Fatal("malformed PDF should return an error")
	}
}

func TestExtractTextFileWithLeadingDashName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "-note.txt")
	if err := os.WriteFile(path, []byte("dash safe"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != "dash safe" {
		t.Errorf("got %q", got)
	}
}
