//go:build !unix

package store

import "time"

// acquireLock is a no-op on platforms without flock. The CLI is single-user and
// this keeps the build portable.
func acquireLock(path string, timeout time.Duration) (func(), error) {
	return func() {}, nil
}
