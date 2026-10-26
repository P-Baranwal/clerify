package store

import "github.com/P-Baranwal/clerify/internal/model"

// migrate upgrades an on-disk store in place. Schema v1 is the initial
// greenfield format, so there are no steps yet; the hooks exist so a future
// version bump is a small, contained change.
func migrate(s *model.Store) {
	if s.SchemaVersion == 0 {
		s.SchemaVersion = 1
	}
}
