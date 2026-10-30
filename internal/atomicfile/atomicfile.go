// Package atomicfile writes files through a temporary sibling and a rename so
// a crash can never leave a partially written target.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFile writes data to target atomically with the given permissions.
func WriteFile(target string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(target)
	f, err := os.CreateTemp(dir, filepath.Base(target)+".tmp.*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if tmp != "" {
			_ = os.Remove(tmp)
		}
	}()

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
