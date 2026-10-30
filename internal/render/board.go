package render

import (
	"fmt"
	"io"

	"github.com/P-Baranwal/clerify/internal/derive"
	"github.com/P-Baranwal/clerify/internal/model"
)

// Board renders a four-column status board.
func Board(w io.Writer, items []model.Item) {
	b := derive.Kanban(items)
	columns := []struct {
		title string
		items []model.Item
	}{
		{"TODO", b.Todo},
		{"DOING", b.InProgress},
		{"BLOCKED", b.Blocked},
		{"DONE", b.Done},
	}
	for _, col := range columns {
		fmt.Fprintf(w, "%s (%d)\n", col.title, len(col.items))
		for _, it := range col.items {
			fmt.Fprintf(w, "  %s  %s\n", ShortID(it.ID), Truncate(it.Title, 56))
		}
		fmt.Fprintln(w)
	}
}
