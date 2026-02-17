// SPDX-License-Identifier: AGPL-3.0-only
package singleton

import (
	"fmt"

	"github.com/gofrs/flock"
)

// Lock represents an acquired singleton lock for a database path.
type Lock struct {
	flock *flock.Flock
}

// TryAcquire attempts to acquire a singleton lock for the given database path.
// It returns the lock and true if acquired (primary instance), or nil and false
// if the lock is already held by another process (secondary instance).
// Secondary instances should exit after their MCP transport closes instead of
// entering keep-alive mode.
func TryAcquire(dbPath string) (*Lock, bool, error) {
	lockPath := dbPath + ".lock"

	fl := flock.New(lockPath)
	locked, err := fl.TryLock()
	if err != nil {
		return nil, false, fmt.Errorf("singleton: try lock %s: %w", lockPath, err)
	}
	if !locked {
		return nil, false, nil
	}
	return &Lock{flock: fl}, true, nil
}

// Release releases the singleton lock.
func (l *Lock) Release() error {
	return l.flock.Unlock()
}
