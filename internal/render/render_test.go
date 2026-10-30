package render

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/P-Baranwal/clerify/internal/model"
)

func resetOptions(t *testing.T) {
	t.Helper()
	SetOptions(Options{TableStyle: "plain", DateFormat: model.DateLayout})
	t.Cleanup(func() { SetOptions(Options{TableStyle: "plain", DateFormat: model.DateLayout}) })
}

func TestWidthHandlesCombiningMarksAndWideRunes(t *testing.T) {
	if got := Width("abc"); got != 3 {
		t.Errorf("Width(abc) = %d", got)
	}
	if got := Width("कि"); got != 1 {
		t.Errorf("Width(Devanagari base + matra) = %d, want 1", got)
	}
	if got := Width("漢"); got != 2 {
		t.Errorf("Width(CJK) = %d, want 2", got)
	}
	if Width("कि") == len("कि") {
		t.Error("combining marks were counted as columns")
	}
}

func TestTableKeepsColumnsAlignedWithDevanagari(t *testing.T) {
	resetOptions(t)
	var buf bytes.Buffer
	Table(&buf, []string{"TITLE", "N"}, [][]string{
		{"असाइनमेंट", "1"},
		{"plain", "22"},
	})
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("unexpected table: %q", buf.String())
	}
	want := Width(lines[0])
	for _, line := range lines {
		if got := Width(line); got != want {
			t.Errorf("line width %d != %d: %q", got, want, line)
		}
	}
}

func TestTableStyleASCII(t *testing.T) {
	resetOptions(t)
	SetOptions(Options{TableStyle: "ascii", DateFormat: model.DateLayout})
	var buf bytes.Buffer
	Table(&buf, []string{"A", "B"}, [][]string{{"1", "2"}})
	if !strings.Contains(buf.String(), " | ") {
		t.Errorf("ascii table has no separators: %q", buf.String())
	}
}

func TestFormatTimestampUsesConfiguredLayout(t *testing.T) {
	resetOptions(t)
	SetOptions(Options{TableStyle: "plain", DateFormat: "02/01/2006"})
	stamp := "2026-11-02T12:00:00Z"
	want := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC).In(time.Local).Format("02/01/2006")
	if got := FormatTimestamp(stamp); got != want {
		t.Errorf("FormatTimestamp = %q, want %q", got, want)
	}
}

func TestColorOption(t *testing.T) {
	resetOptions(t)
	items := []model.Item{model.NewItem("overdue")}
	due := "2020-01-01"
	items[0].Deadline = &due

	SetOptions(Options{Color: true, TableStyle: "plain", DateFormat: model.DateLayout})
	var colored bytes.Buffer
	ItemTable(&colored, items, nil, time.Now())
	if !strings.Contains(colored.String(), "\x1b[31m") {
		t.Errorf("expected red overdue cell: %q", colored.String())
	}

	SetOptions(Options{Color: false, TableStyle: "plain", DateFormat: model.DateLayout})
	var plain bytes.Buffer
	ItemTable(&plain, items, nil, time.Now())
	if strings.Contains(plain.String(), "\x1b[") {
		t.Errorf("color leaked without the option: %q", plain.String())
	}
}
