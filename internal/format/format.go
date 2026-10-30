// Package format holds the small numeric renderers shared by the CLI and
// report renderers.
package format

import (
	"strconv"
	"strings"
)

// Number renders a float without trailing zeros or a fixed precision.
func Number(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// TrimFloat renders a float with up to two decimals, trimming trailing zeros.
func TrimFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ".")
}

// Hours renders a duration in hours, e.g. "2.5h".
func Hours(v float64) string {
	return TrimFloat(v) + "h"
}
