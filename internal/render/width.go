package render

import (
	"regexp"
	"unicode"

	"github.com/mattn/go-runewidth"
)

var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

// Width returns the number of terminal columns s occupies. Combining marks
// (common in Devanagari) and zero-width format characters count as zero.
func Width(s string) int {
	s = ansiPattern.ReplaceAllString(s, "")
	width := 0
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		width += runewidth.RuneWidth(r)
	}
	return width
}

// PadRight pads s with spaces to width terminal columns.
func PadRight(s string, width int) string {
	n := width - Width(s)
	if n <= 0 {
		return s
	}
	return s + spaces(n)
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}
