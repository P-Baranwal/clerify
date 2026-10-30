package store

import (
	"time"

	"github.com/P-Baranwal/clerify/internal/model"
)

// migrate upgrades an on-disk store in place.
func migrate(s *model.Store) {
	if s.SchemaVersion == 0 {
		s.SchemaVersion = 1
	}
	if s.SchemaVersion < 2 {
		for i := range s.Items {
			it := &s.Items[i]
			if it.Status != model.StatusDone || it.CompletedAt != nil {
				continue
			}
			ts := it.UpdatedAt
			if ts == "" {
				ts = it.CreatedAt
			}
			if ts != "" {
				it.CompletedAt = &ts
			}
		}
		s.SchemaVersion = 2
	}
}

// Upgrade applies migrations and normalization to an imported document and
// returns any data warnings.
func Upgrade(s *model.Store) []string {
	migrate(s)
	return sanitizeAt(s, time.Now())
}
