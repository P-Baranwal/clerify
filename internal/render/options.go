package render

import (
	"strings"
	"time"

	"github.com/P-Baranwal/clerify/internal/model"
)

// Options controls presentation without changing stored data.
type Options struct {
	Color      bool
	TableStyle string
	DateFormat string
}

var current = Options{TableStyle: "plain", DateFormat: model.DateLayout}

// SetOptions installs the process-wide presentation options.
func SetOptions(o Options) {
	switch o.TableStyle {
	case "plain", "ascii", "unicode":
	default:
		o.TableStyle = "plain"
	}
	switch o.DateFormat {
	case model.DateLayout, "02/01/2006", "Jan 2, 2006":
	default:
		o.DateFormat = model.DateLayout
	}
	current = o
}

// FormatTimestamp renders a stored RFC3339 timestamp with the configured
// display layout. Storage always remains RFC3339.
func FormatTimestamp(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.Local().Format(current.DateFormat)
}

func colorize(s, code string) string {
	if !current.Color || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func colorUrgency(s string) string {
	switch s {
	case "overdue":
		return colorize(s, "31")
	case "today", "tmrw":
		return colorize(s, "33")
	case "soon":
		return colorize(s, "36")
	}
	return s
}

func colorStatus(s string) string {
	switch s {
	case "done":
		return colorize(s, "32")
	case "doing":
		return colorize(s, "33")
	case "blocked":
		return colorize(s, "31")
	}
	return s
}
