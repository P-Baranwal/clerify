package store

import (
	"os"

	"github.com/P-Baranwal/clerify/internal/atomicfile"
)

// writeFileAtomic writes data to a temporary sibling and renames it into place
// so a crash can never leave a partially written file.
func writeFileAtomic(target string, data []byte, perm os.FileMode) error {
	return atomicfile.WriteFile(target, data, perm)
}

// backupCurrent copies the existing store to the backup path. It is a no-op
// when the store does not exist yet.
func backupCurrent(target, backup string) error {
	data, err := os.ReadFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return atomicfile.WriteFile(backup, data, 0o644)
}
