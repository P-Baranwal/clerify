package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeFileAtomic writes data to a temporary sibling and renames it into place
// so a crash can never leave a partially written file.
func writeFileAtomic(target string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(target)
	f, err := os.CreateTemp(dir, filepath.Base(target)+".tmp.*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	cleanup := func() {
		if tmp != "" {
			_ = os.Remove(tmp)
		}
	}
	defer cleanup()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("rename %s: %w", target, err)
	}
	tmp = ""
	return nil
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
	return writeFileAtomic(backup, data, 0o644)
}
