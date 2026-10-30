package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/P-Baranwal/clerify/internal/derive"
	"github.com/P-Baranwal/clerify/internal/model"
	"github.com/P-Baranwal/clerify/internal/store"
)

// referenceItems is the stable order behind `#N` references: open items by
// priority. Showing the number in `ls` lets people type `clerify done #3`
// instead of a hex prefix. Completed items are addressed by id or title.
func referenceItems(items []model.Item) []model.Item {
	return derive.PriorityList(items)
}

func referenceNumbers(items []model.Item) map[string]int {
	out := make(map[string]int, len(items))
	for i, it := range referenceItems(items) {
		out[it.ID] = i + 1
	}
	return out
}

// resolveItemRef resolves an id prefix, a `#N` row reference, a bare row
// number (when no id matches), or an item title. Titles match exactly first,
// then as a unique substring.
func (a *App) resolveItemRef(ref string) (model.Item, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return model.Item{}, fail(3, errors.New("item reference is empty"))
	}

	if n, ok := parseHashNumber(ref); ok {
		return a.itemByNumber(n)
	}

	it, err := a.DB.FindItem(ref)
	if err == nil {
		return it, nil
	}
	var ambiguous *store.AmbiguousError
	if errors.As(err, &ambiguous) {
		return model.Item{}, fail(3, err)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return model.Item{}, err
	}

	if n, ok := parseBareNumber(ref); ok {
		if it, err := a.itemByNumber(n); err == nil {
			return it, nil
		}
	}

	if matches := itemsMatchingTitle(a.DB.Items(), ref, true); len(matches) > 0 {
		return a.pickOrReport(ref, matches)
	}
	if matches := itemsMatchingTitle(a.DB.Items(), ref, false); len(matches) > 0 {
		return a.pickOrReport(ref, matches)
	}
	return model.Item{}, fail(3, fmt.Errorf("item %q not found; run `clerify ls` to see items", ref))
}

func (a *App) itemByNumber(n int) (model.Item, error) {
	items := referenceItems(a.DB.Items())
	if n < 1 || n > len(items) {
		return model.Item{}, fail(3, fmt.Errorf("item #%d not found (there are %d items; run `clerify ls` to see the numbers)", n, len(items)))
	}
	return items[n-1], nil
}

func (a *App) pickOrReport(ref string, matches []model.Item) (model.Item, error) {
	if len(matches) == 1 {
		return matches[0], nil
	}
	if a.isInteractive() {
		fmt.Fprintf(a.Err, "%q matches %d items:\n", ref, len(matches))
		for i, it := range matches {
			fmt.Fprintf(a.Err, "  %d. %s  %s\n", i+1, shortID(it.ID), it.Title)
		}
		fmt.Fprint(a.Err, "Choose one [1-N]: ")
		line, _ := bufio.NewReader(a.Stdin).ReadString('\n')
		n, err := strconv.Atoi(strings.TrimSpace(line))
		if err == nil && n >= 1 && n <= len(matches) {
			return matches[n-1], nil
		}
	}
	return model.Item{}, ambiguousItemError(ref, matches)
}

func itemsMatchingTitle(items []model.Item, ref string, exact bool) []model.Item {
	lower := strings.ToLower(strings.TrimSpace(ref))
	var out []model.Item
	for _, it := range items {
		title := strings.ToLower(it.Title)
		if (exact && title == lower) || (!exact && strings.Contains(title, lower)) {
			out = append(out, it)
		}
	}
	return out
}

func ambiguousItemError(ref string, items []model.Item) error {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%s (%s)", shortID(it.ID), it.Title))
	}
	return fail(3, fmt.Errorf("item %q is ambiguous; matches: %s", ref, strings.Join(parts, ", ")))
}

func parseHashNumber(ref string) (int, bool) {
	if !strings.HasPrefix(ref, "#") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(ref[1:]))
	return n, err == nil
}

func parseBareNumber(ref string) (int, bool) {
	if ref == "" {
		return 0, false
	}
	for _, r := range ref {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(ref)
	return n, err == nil
}

// isInteractive reports whether stdin is a terminal we can prompt on.
func (a *App) isInteractive() bool {
	f, ok := a.Stdin.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
