//go:build windows

package store

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
)

const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002
)

// acquireLock takes an advisory exclusive lock using LockFileEx, retrying
// until timeout.
func acquireLock(path string, timeout time.Duration) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	var overlapped windows.Overlapped
	deadline := time.Now().Add(timeout)
	for {
		err = windows.LockFileEx(
			windows.Handle(f.Fd()),
			lockfileExclusiveLock|lockfileFailImmediately,
			0, 1, 0, &overlapped,
		)
		if err == nil {
			break
		}
		if err != windows.ERROR_LOCK_VIOLATION {
			_ = f.Close()
			return nil, err
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, ErrLockTimeout
		}
		time.Sleep(20 * time.Millisecond)
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &overlapped)
		_ = f.Close()
	}, nil
}
